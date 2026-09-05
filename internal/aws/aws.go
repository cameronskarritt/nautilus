package aws

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"

	"nautilus/internal/config"
)

// LoadConfig builds an aws.Config from environment variables.
// When AWS_ENDPOINT_URL is set (e.g. LocalStack), static credentials are used.
// Otherwise the default credential chain is used (IAM roles, env, etc.).
func LoadConfig(ctx context.Context) (aws.Config, error) {
	region := config.Get("AWS_REGION", "us-east-1")
	endpoint := config.Get[string]("AWS_ENDPOINT_URL")
	accessKey := ""
	secretKey := ""
	if endpoint != "" {
		accessKey = config.Get("AWS_ACCESS_KEY_ID", "test")
		secretKey = config.Get("AWS_SECRET_ACCESS_KEY", "test")
	}
	return loadSDKConfig(ctx, region, endpoint, accessKey, secretKey)
}
