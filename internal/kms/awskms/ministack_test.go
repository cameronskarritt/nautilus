package awskms_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/kms"

	"nautilus/internal/crypto/encrypt"
	"nautilus/internal/kms/awskms"
	"nautilus/internal/testutil"
	"nautilus/internal/testutil/require"
)

// MiniStack exercises the SDK protocol, not AWS's cryptographic guarantees.
func TestManagerMiniStack(t *testing.T) {
	endpoint := os.Getenv("KMS_TEST_ENDPOINT")
	if endpoint == "" {
		t.Skip("set KMS_TEST_ENDPOINT to a local MiniStack endpoint")
	}
	cfg := aws.Config{
		Region:       "us-east-1",
		BaseEndpoint: aws.String(endpoint),
		Credentials:  credentials.NewStaticCredentialsProvider("test", "test", ""),
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	client := kms.NewFromConfig(cfg)
	newKey := func() string {
		out, err := client.CreateKey(ctx, &kms.CreateKeyInput{Description: aws.String("Nautilus local KMS integration test")})
		require.NoError(t, err)
		require.NotNil(t, out.KeyMetadata)
		arn := aws.ToString(out.KeyMetadata.Arn)
		t.Cleanup(func() {
			cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cleanupCancel()
			_, err := client.ScheduleKeyDeletion(cleanupCtx, &kms.ScheduleKeyDeletionInput{KeyId: &arn, PendingWindowInDays: aws.Int32(7)})
			if err != nil {
				t.Errorf("schedule local test key deletion: %v", err)
			}
		})
		return arn
	}
	db := testutil.SetupTestDB(t)
	m := awskms.New(cfg, db)
	org := createOrg(t, db, "ministack")
	require.NoError(t, m.ProvisionOrganization(ctx, org.ExternalID, newKey()))
	require.NoError(t, m.ProvisionUser(ctx, newKey()))
	binding := encrypt.Binding{Purpose: "totp", RecordID: "test-user"}
	ciphertext, err := encrypt.ForUser(m).Seal(ctx, []byte("synthetic TOTP secret"), binding)
	require.NoError(t, err)
	key, err := m.OrganizationKey(ctx, org.ExternalID)
	require.NoError(t, err)
	defer clear(key)
	require.Len(t, key, 32)
	fresh := awskms.New(cfg, db)
	got, err := fresh.OrganizationKey(ctx, org.ExternalID)
	require.NoError(t, err)
	defer clear(got)
	require.Equal(t, key, got)
	userKey, err := fresh.UserKey(ctx)
	require.NoError(t, err)
	defer clear(userKey)
	require.Len(t, userKey, 32)
	require.NotEqual(t, key, userKey)
	plaintext, err := encrypt.ForUser(fresh).Open(ctx, ciphertext, binding)
	require.NoError(t, err)
	defer clear(plaintext)
	require.Equal(t, "synthetic TOTP secret", string(plaintext))
}
