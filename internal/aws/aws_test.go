package aws

import (
	"context"
	"os"
	"testing"

	"nautilus/internal/testutil/require"
)

func TestLoadConfigUsesStaticCredentialsForCustomEndpoint(t *testing.T) {
	defer setenv(t, "AWS_REGION", "us-west-2")()
	defer setenv(t, "AWS_ENDPOINT_URL", "http://localhost:4566")()
	defer setenv(t, "AWS_ACCESS_KEY_ID", "local-access")()
	defer setenv(t, "AWS_SECRET_ACCESS_KEY", "local-secret")()

	cfg, err := LoadConfig(context.Background())
	require.NoError(t, err)

	require.Equal(t, "us-west-2", cfg.Region)
	require.NotNil(t, cfg.BaseEndpoint)
	require.Equal(t, "http://localhost:4566", *cfg.BaseEndpoint)

	creds, err := cfg.Credentials.Retrieve(context.Background())
	require.NoError(t, err)
	require.Equal(t, "local-access", creds.AccessKeyID)
	require.Equal(t, "local-secret", creds.SecretAccessKey)
}

func setenv(t *testing.T, key, value string) func() {
	t.Helper()

	err := os.Setenv(key, value)
	require.NoError(t, err)

	return func() {
		err := os.Unsetenv(key)
		require.NoError(t, err)
	}
}
