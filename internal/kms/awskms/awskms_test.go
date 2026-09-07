package awskms_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/smithy-go"
	"github.com/aws/smithy-go/logging"

	"nautilus/internal/database"
	"nautilus/internal/database/kmskeys"
	"nautilus/internal/database/organizations"
	"nautilus/internal/kms"
	"nautilus/internal/kms/awskms"
	"nautilus/internal/testutil"
	"nautilus/internal/testutil/require"
)

var _ kms.KeyManager = (*awskms.Manager)(nil)

const (
	orgARN   = "arn:aws:kms:us-east-1:123456789012:key/11111111-1111-1111-1111-111111111111"
	userARN  = "arn:aws:kms:us-east-1:123456789012:key/22222222-2222-2222-2222-222222222222"
	otherARN = "arn:aws:kms:us-east-1:123456789012:key/33333333-3333-3333-3333-333333333333"
)

func TestManagerKeys(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	f := newProvider(t)
	m := awskms.New(f.cfg, db)
	org := createOrg(t, db, "first")
	other := createOrg(t, db, "second")
	require.NoError(t, m.ProvisionOrganization(t.Context(), org.ExternalID, orgARN))
	require.NoError(t, m.ProvisionOrganization(t.Context(), other.ExternalID, otherARN))
	require.NoError(t, m.ProvisionUser(t.Context(), userARN))

	key, err := m.OrganizationKey(t.Context(), strings.ToUpper(org.ExternalID))
	require.NoError(t, err)
	require.Len(t, key, 32)
	userKey, err := m.UserKey(t.Context())
	require.NoError(t, err)
	otherKey, err := m.OrganizationKey(t.Context(), other.ExternalID)
	require.NoError(t, err)
	require.NotEqual(t, key, userKey)
	require.NotEqual(t, key, otherKey)
	require.NotEqual(t, otherKey, userKey)

	want := bytes.Clone(key)
	clear(key)
	fresh := awskms.New(f.cfg, db)
	got, err := fresh.OrganizationKey(t.Context(), org.ExternalID)
	require.NoError(t, err)
	require.Equal(t, want, got)
	got, err = fresh.UserKey(t.Context())
	require.NoError(t, err)
	require.Equal(t, userKey, got)
	stored, err := kmskeys.GetOrganization(t.Context(), db, org.ID)
	require.NoError(t, err)
	require.NotEqual(t, want, stored.Ciphertext)
	require.Equal(t, orgARN, stored.ProviderKeyID)

	f.mu.Lock()
	defer f.mu.Unlock()
	for _, req := range f.requests {
		wantContext := map[string]string{"application": "nautilus", "format": "1", "scope": "users"}
		if req.KeyID != userARN {
			wantContext["scope"] = "organization"
			wantContext["organization_id"] = org.ExternalID
			if req.KeyID == otherARN {
				wantContext["organization_id"] = other.ExternalID
			}
		}
		require.Equal(t, wantContext, req.EncryptionContext)
	}
}

func TestManagerRejectsTransplantedCiphertext(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	f := newProvider(t)
	m := awskms.New(f.cfg, db)
	org := createOrg(t, db, "source")
	other := createOrg(t, db, "destination")
	require.NoError(t, m.ProvisionOrganization(t.Context(), org.ExternalID, orgARN))
	source, err := kmskeys.GetOrganization(t.Context(), db, org.ID)
	require.NoError(t, err)
	_, err = kmskeys.CreateOrganization(t.Context(), db, other.ID, otherARN, source.Ciphertext)
	require.NoError(t, err)
	_, err = m.OrganizationKey(t.Context(), other.ExternalID)
	var apiErr smithy.APIError
	require.ErrorAs(t, err, &apiErr)
	require.Equal(t, "InvalidCiphertextException", apiErr.ErrorCode())

	// Even retaining the provider key cannot move a blob to the shared user scope.
	_, err = db.Exec(t.Context(), "DELETE FROM kms_keys WHERE organization_id = $1", org.ID)
	require.NoError(t, err)
	_, err = kmskeys.CreateUser(t.Context(), db, orgARN, source.Ciphertext)
	require.NoError(t, err)
	_, err = m.UserKey(t.Context())
	require.ErrorAs(t, err, &apiErr)
	require.Equal(t, "InvalidCiphertextException", apiErr.ErrorCode())
}

func TestManagerMissingKeys(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	f := newProvider(t)
	m := awskms.New(f.cfg, db)
	org := createOrg(t, db, "deleted")
	_, err := m.UserKey(t.Context())
	require.Error(t, err)
	_, err = m.OrganizationKey(t.Context(), org.ExternalID)
	require.Error(t, err)
	_, err = m.OrganizationKey(t.Context(), "00000000-0000-0000-0000-000000000000")
	require.Error(t, err)
	require.Zero(t, f.count())

	require.NoError(t, m.ProvisionOrganization(t.Context(), org.ExternalID, orgARN))
	require.NoError(t, organizations.Delete(t.Context(), db, org.ID))
	_, err = m.OrganizationKey(t.Context(), org.ExternalID)
	require.Error(t, err)
	require.NoError(t, m.ProvisionUser(t.Context(), userARN))
	_, err = m.OrganizationKey(t.Context(), org.ExternalID)
	require.Error(t, err)
	require.Equal(t, 2, f.count())
}

func TestProvisionIdempotent(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	f := newProvider(t)
	m := awskms.New(f.cfg, db)
	org := createOrg(t, db, "idempotent")
	for range 2 {
		require.NoError(t, m.ProvisionOrganization(t.Context(), org.ExternalID, orgARN))
		require.NoError(t, m.ProvisionUser(t.Context(), userARN))
	}
	require.Error(t, m.ProvisionOrganization(t.Context(), org.ExternalID, otherARN))
	require.Error(t, m.ProvisionUser(t.Context(), otherARN))
	require.Equal(t, 2, f.count())
}

func TestConcurrentProvision(t *testing.T) {
	t.Parallel()
	for _, scope := range []string{"users", "organization"} {
		t.Run(scope, func(t *testing.T) {
			t.Parallel()
			db := testutil.SetupTestDBWithCommit(t)
			f := newProvider(t)
			m := awskms.New(f.cfg, db)
			provision := func() error { return m.ProvisionUser(t.Context(), userARN) }
			lookup := func() ([]byte, error) { return awskms.New(f.cfg, db).UserKey(t.Context()) }
			if scope == "organization" {
				org := createOrg(t, db, "concurrent")
				provision = func() error { return m.ProvisionOrganization(t.Context(), org.ExternalID, orgARN) }
				lookup = func() ([]byte, error) { return awskms.New(f.cfg, db).OrganizationKey(t.Context(), org.ExternalID) }
			}
			ready, release := f.barrier("GenerateDataKeyWithoutPlaintext")
			results := make(chan error, 2)
			for range 2 {
				go func() { results <- provision() }()
			}
			<-ready
			<-ready
			close(release)
			for range 2 {
				require.NoError(t, <-results)
			}
			want, err := lookup()
			require.NoError(t, err)
			got, err := lookup()
			require.NoError(t, err)
			require.Equal(t, want, got)
		})
	}
}

func TestConcurrentImport(t *testing.T) {
	t.Parallel()
	for _, same := range []bool{true, false} {
		t.Run(fmt.Sprint(same), func(t *testing.T) {
			t.Parallel()
			db := testutil.SetupTestDBWithCommit(t)
			f := newProvider(t)
			m := awskms.New(f.cfg, db)
			first := bytes.Repeat([]byte{42}, 32)
			second := bytes.Clone(first)
			if !same {
				second[0]++
			}
			ready, release := f.barrier("Encrypt")
			results := make(chan error, 2)
			for _, key := range [][]byte{first, second} {
				go func() { results <- m.ImportUserKey(t.Context(), userARN, key) }()
			}
			<-ready
			<-ready
			close(release)
			successes := 0
			for range 2 {
				if <-results == nil {
					successes++
				}
			}
			if same {
				require.Equal(t, 2, successes)
			} else {
				require.Equal(t, 1, successes)
			}
			got, err := m.UserKey(t.Context())
			require.NoError(t, err)
			require.True(t, bytes.Equal(first, got) || bytes.Equal(second, got))
		})
	}
}

func TestImportUserKey(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	f := newProvider(t)
	m := awskms.New(f.cfg, db)
	key := bytes.Repeat([]byte{42}, 32)
	for range 2 {
		require.NoError(t, m.ImportUserKey(t.Context(), userARN, key))
	}
	got, err := awskms.New(f.cfg, db).UserKey(t.Context())
	require.NoError(t, err)
	require.Equal(t, key, got)
	require.Equal(t, key, bytes.Repeat([]byte{42}, 32))
	require.Error(t, m.ImportUserKey(t.Context(), userARN, bytes.Repeat([]byte{43}, 32)))
	require.Error(t, m.ImportUserKey(t.Context(), otherARN, key))
	got, err = m.UserKey(t.Context())
	require.NoError(t, err)
	require.Equal(t, key, got)
}

func TestInvalidProvisionInput(t *testing.T) {
	t.Parallel()
	for _, arn := range []string{"", "alias/users", "11111111-1111-1111-1111-111111111111", "arn:aws:kms:us-east-1:123456789012:alias/users", " " + userARN} {
		t.Run(arn, func(t *testing.T) {
			t.Parallel()
			m := awskms.New(aws.Config{}, nil)
			require.Error(t, m.ProvisionUser(t.Context(), arn))
			require.Error(t, m.ProvisionOrganization(t.Context(), "org", arn))
			require.Error(t, m.ImportUserKey(t.Context(), arn, make([]byte, 32)))
		})
	}
	for _, size := range []int{0, 16, 31, 33} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			t.Parallel()
			m := awskms.New(aws.Config{}, nil)
			require.Error(t, m.ImportUserKey(t.Context(), userARN, make([]byte, size)))
		})
	}
}

func TestInvalidProviderResponse(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		op       string
		response map[string]any
	}{
		{"generated key ARN", "GenerateDataKeyWithoutPlaintext", map[string]any{"KeyId": otherARN, "CiphertextBlob": []byte{1}}},
		{"generated empty blob", "GenerateDataKeyWithoutPlaintext", map[string]any{"KeyId": userARN}},
		{"imported key ARN", "Encrypt", map[string]any{"KeyId": otherARN, "CiphertextBlob": []byte{1}}},
		{"imported empty blob", "Encrypt", map[string]any{"KeyId": userARN}},
		{"decrypted key ARN", "Decrypt", map[string]any{"KeyId": otherARN, "Plaintext": make([]byte, 32)}},
		{"decrypted short key", "Decrypt", map[string]any{"KeyId": userARN, "Plaintext": make([]byte, 31)}},
		{"decrypted long key", "Decrypt", map[string]any{"KeyId": userARN, "Plaintext": make([]byte, 33)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			db := testutil.SetupTestDB(t)
			f := newProvider(t)
			f.response = func(op string, req request) (any, int) {
				if op == tt.op {
					return tt.response, http.StatusOK
				}
				return nil, 0
			}
			m := awskms.New(f.cfg, db)
			switch tt.op {
			case "GenerateDataKeyWithoutPlaintext":
				require.Error(t, m.ProvisionUser(t.Context(), userARN))
			case "Encrypt":
				require.Error(t, m.ImportUserKey(t.Context(), userARN, make([]byte, 32)))
			case "Decrypt":
				require.NoError(t, m.ProvisionUser(t.Context(), userARN))
				key, err := m.UserKey(t.Context())
				require.Error(t, err)
				require.Nil(t, key)
			}
		})
	}
}

func TestProviderFailureRetry(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	f := newProvider(t)
	f.response = func(op string, req request) (any, int) {
		if len(f.requests) == 1 {
			return map[string]string{"__type": "DisabledException", "message": "key disabled"}, http.StatusBadRequest
		}
		return nil, 0
	}
	m := awskms.New(f.cfg, db)
	err := m.ProvisionUser(t.Context(), userARN)
	var apiErr smithy.APIError
	require.ErrorAs(t, err, &apiErr)
	require.Equal(t, "DisabledException", apiErr.ErrorCode())
	key, err := kmskeys.GetUser(t.Context(), db)
	require.NoError(t, err)
	require.Nil(t, key)
	require.NoError(t, m.ProvisionUser(t.Context(), userARN))
	got, err := m.UserKey(t.Context())
	require.NoError(t, err)
	require.Len(t, got, 32)
}

func TestProviderCancellation(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	f := newProvider(t)
	started := make(chan struct{})
	release := make(chan struct{})
	f.response = func(op string, req request) (any, int) {
		if op == "Decrypt" {
			close(started)
			<-release
		}
		return nil, 0
	}
	m := awskms.New(f.cfg, db)
	require.NoError(t, m.ProvisionUser(t.Context(), userARN))
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	result := make(chan error, 1)
	go func() {
		_, err := m.UserKey(ctx)
		result <- err
	}()
	<-started
	cancel()
	err := <-result
	close(release)
	require.ErrorIs(t, err, context.Canceled)
}

func TestKeyBodiesNotLogged(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	f := newProvider(t)
	var logs bytes.Buffer
	f.cfg.Logger = logging.NewStandardLogger(&logs)
	f.cfg.ClientLogMode = aws.LogRequestWithBody | aws.LogResponseWithBody
	m := awskms.New(f.cfg, db)
	key := bytes.Repeat([]byte{42}, 32)
	require.NoError(t, m.ImportUserKey(t.Context(), userARN, key))
	got, err := m.UserKey(t.Context())
	require.NoError(t, err)
	require.Equal(t, key, got)
	require.NotContains(t, logs.String(), base64.StdEncoding.EncodeToString(key))
	require.NotContains(t, logs.String(), "Plaintext")
}

func createOrg(t *testing.T, db database.Database, name string) *organizations.Organization {
	t.Helper()
	id := testutil.CreateTestOrg(t, db, name, name)
	org, err := organizations.Get(t.Context(), db, id)
	require.NoError(t, err)
	return org
}

type request struct {
	KeyID               string `json:"KeyId"`
	EncryptionContext   map[string]string
	CiphertextBlob      []byte
	Plaintext           []byte
	KeySpec             string
	EncryptionAlgorithm string
}

type wrappedKey struct {
	keyID     string
	binding   map[string]string
	plaintext []byte
}

// The fake implements the AWS JSON protocol and enforces key/context binding;
// its opaque blob registry is not intended to emulate KMS cryptography.
type provider struct {
	mu       sync.Mutex
	cfg      aws.Config
	keys     map[string]wrappedKey
	requests []request
	response func(string, request) (any, int)
	before   func(string)
}

func newProvider(t *testing.T) *provider {
	t.Helper()
	f := &provider{keys: make(map[string]wrappedKey)}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		op := strings.TrimPrefix(r.Header.Get("X-Amz-Target"), "TrentService.")
		if f.before != nil {
			f.before(op)
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		var req request
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		f.requests = append(f.requests, req)
		w.Header().Set("Content-Type", "application/x-amz-json-1.1")
		var out any
		status := http.StatusOK
		if f.response != nil {
			out, status = f.response(op, req)
		}
		if out == nil {
			status = http.StatusOK
			switch op {
			case "GenerateDataKeyWithoutPlaintext", "Encrypt":
				plain := req.Plaintext
				if op == "GenerateDataKeyWithoutPlaintext" {
					if req.KeySpec != "AES_256" {
						t.Errorf("unexpected key spec: %s", req.KeySpec)
					}
					plain = make([]byte, 32)
					if _, err := rand.Read(plain); err != nil {
						t.Error(err)
						w.WriteHeader(http.StatusInternalServerError)
						return
					}
				}
				blob := []byte(fmt.Sprintf("wrapped-%d", len(f.keys)))
				f.keys[string(blob)] = wrappedKey{req.KeyID, req.EncryptionContext, plain}
				out = map[string]any{"KeyId": req.KeyID, "CiphertextBlob": blob}
			case "Decrypt":
				if req.EncryptionAlgorithm != "SYMMETRIC_DEFAULT" {
					t.Errorf("unexpected encryption algorithm: %s", req.EncryptionAlgorithm)
				}
				key, ok := f.keys[string(req.CiphertextBlob)]
				if !ok || key.keyID != req.KeyID || !reflect.DeepEqual(key.binding, req.EncryptionContext) {
					status = http.StatusBadRequest
					out = map[string]string{"__type": "InvalidCiphertextException", "message": "binding mismatch"}
				} else {
					out = map[string]any{"KeyId": key.keyID, "Plaintext": key.plaintext}
				}
			default:
				t.Errorf("unexpected KMS operation: %s", op)
				status = http.StatusBadRequest
				out = map[string]string{"__type": "UnsupportedOperationException"}
			}
		}
		w.WriteHeader(status)
		if err := json.NewEncoder(w).Encode(out); err != nil {
			t.Error(err)
		}
	}))
	t.Cleanup(server.Close)
	f.cfg = aws.Config{
		Region:           "us-east-1",
		BaseEndpoint:     aws.String(server.URL),
		Credentials:      credentials.NewStaticCredentialsProvider("test", "test", ""),
		RetryMaxAttempts: 1,
	}
	return f
}

func (f *provider) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.requests)
}

func (f *provider) barrier(operation string) (chan struct{}, chan struct{}) {
	ready := make(chan struct{}, 2)
	release := make(chan struct{})
	f.before = func(op string) {
		if op == operation {
			ready <- struct{}{}
			<-release
		}
	}
	return ready, release
}
