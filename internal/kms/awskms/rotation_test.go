package awskms_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/aws/smithy-go"

	"nautilus/internal/crypto/encrypt"
	"nautilus/internal/database/kmskeys"
	"nautilus/internal/database/organizations"
	"nautilus/internal/kms/awskms"
	"nautilus/internal/testutil"
	"nautilus/internal/testutil/require"
)

func TestRotationPreservesApplicationKeys(t *testing.T) {
	t.Parallel()
	for _, scope := range []string{"organization", "users"} {
		t.Run(scope, func(t *testing.T) {
			t.Parallel()
			db := testutil.SetupTestDB(t)
			f := newProvider(t)
			m := awskms.New(f.cfg, db)
			org := createOrg(t, db, "rotate")
			other := createOrg(t, db, "other")
			require.NoError(t, m.ProvisionOrganization(t.Context(), org.ExternalID, orgARN))
			require.NoError(t, m.ProvisionOrganization(t.Context(), other.ExternalID, otherARN))
			require.NoError(t, m.ProvisionUser(t.Context(), userARN))
			wantARN := userARN
			lookup := func() ([]byte, error) { return awskms.New(f.cfg, db).UserKey(t.Context()) }
			rotate := func() error { return m.RotateUser(t.Context()) }
			if scope == "organization" {
				wantARN = orgARN
				lookup = func() ([]byte, error) { return awskms.New(f.cfg, db).OrganizationKey(t.Context(), org.ExternalID) }
				rotate = func() error { return m.RotateOrganization(t.Context(), strings.ToUpper(org.ExternalID)) }
			}
			beforeOrg, err := kmskeys.GetOrganization(t.Context(), db, org.ID)
			require.NoError(t, err)
			beforeOther, err := kmskeys.GetOrganization(t.Context(), db, other.ID)
			require.NoError(t, err)
			beforeUser, err := kmskeys.GetUser(t.Context(), db)
			require.NoError(t, err)
			key, err := lookup()
			require.NoError(t, err)
			defer clear(key)
			enc := encrypt.ForUser(m)
			if scope == "organization" {
				enc = encrypt.ForOrganization(m, org.ExternalID)
			}
			binding := encrypt.Binding{Purpose: "rotation-test", RecordID: "record"}
			envelope, err := enc.Seal(t.Context(), []byte("synthetic secret"), binding)
			require.NoError(t, err)
			calls := 0
			f.response = func(op string, req request) (any, int) {
				if op != "RotateKeyOnDemand" {
					return nil, 0
				}
				calls++
				require.Equal(t, wantARN, req.KeyID)
				require.Empty(t, req.CiphertextBlob)
				require.Empty(t, req.EncryptionContext)
				return map[string]string{"KeyId": req.KeyID}, http.StatusOK
			}
			require.NoError(t, rotate())
			require.Equal(t, 1, calls)
			afterOrg, err := kmskeys.GetOrganization(t.Context(), db, org.ID)
			require.NoError(t, err)
			afterOther, err := kmskeys.GetOrganization(t.Context(), db, other.ID)
			require.NoError(t, err)
			afterUser, err := kmskeys.GetUser(t.Context(), db)
			require.NoError(t, err)
			require.Equal(t, beforeOrg, afterOrg)
			require.Equal(t, beforeOther, afterOther)
			require.Equal(t, beforeUser, afterUser)
			freshKey, err := lookup()
			require.NoError(t, err)
			defer clear(freshKey)
			require.Equal(t, key, freshKey)
			fresh := encrypt.ForUser(awskms.New(f.cfg, db))
			if scope == "organization" {
				fresh = encrypt.ForOrganization(awskms.New(f.cfg, db), org.ExternalID)
			}
			plaintext, err := fresh.Open(t.Context(), envelope, binding)
			require.NoError(t, err)
			require.Equal(t, "synthetic secret", string(plaintext))
			clear(plaintext)
			after, err := fresh.Seal(t.Context(), []byte("after rotation"), binding)
			require.NoError(t, err)
			plaintext, err = enc.Open(t.Context(), after, binding)
			require.NoError(t, err)
			require.Equal(t, "after rotation", string(plaintext))
			clear(plaintext)

		})
	}
}

func TestRotationRejectsUnavailableKeys(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"missing user key", "missing organization", "missing organization key", "deleted organization", "invalid ARN"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			db := testutil.SetupTestDB(t)
			f := newProvider(t)
			m := awskms.New(f.cfg, db)
			org := createOrg(t, db, "unavailable")
			rotate := func() error { return m.RotateOrganization(t.Context(), org.ExternalID) }
			switch name {
			case "missing user key":
				rotate = func() error { return m.RotateUser(t.Context()) }
			case "missing organization":
				rotate = func() error { return m.RotateOrganization(t.Context(), "00000000-0000-0000-0000-000000000000") }
			case "deleted organization":
				_, err := kmskeys.CreateOrganization(t.Context(), db, org.ID, orgARN, []byte("wrapped"))
				require.NoError(t, err)
				require.NoError(t, organizations.Delete(t.Context(), db, org.ID))
			case "invalid ARN":
				_, err := kmskeys.CreateOrganization(t.Context(), db, org.ID, "alias/organization", []byte("wrapped"))
				require.NoError(t, err)
			}
			require.Error(t, rotate())
			require.Zero(t, f.count())
		})
	}
}

func TestRotationFailureDoesNotRetryOrWrite(t *testing.T) {
	t.Parallel()
	for _, code := range []string{"UnsupportedOperationException", "DisabledException", "KMSInternalException"} {
		t.Run(code, func(t *testing.T) {
			t.Parallel()
			db := testutil.SetupTestDB(t)
			f := newProvider(t)
			f.cfg.RetryMaxAttempts = 3
			m := awskms.New(f.cfg, db)
			require.NoError(t, m.ProvisionUser(t.Context(), userARN))
			before, err := kmskeys.GetUser(t.Context(), db)
			require.NoError(t, err)
			f.response = func(op string, req request) (any, int) {
				require.Equal(t, "RotateKeyOnDemand", op)
				return map[string]string{"__type": code, "message": "rotation unavailable"}, http.StatusInternalServerError
			}
			err = m.RotateUser(t.Context())
			var apiErr smithy.APIError
			require.ErrorAs(t, err, &apiErr)
			require.Equal(t, code, apiErr.ErrorCode())
			require.Contains(t, err.Error(), "check provider rotation status before retrying")
			require.Equal(t, 2, f.count())
			after, err := kmskeys.GetUser(t.Context(), db)
			require.NoError(t, err)
			require.Equal(t, before, after)
		})
	}
}
