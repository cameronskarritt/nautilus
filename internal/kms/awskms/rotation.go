package awskms

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/kms"

	"nautilus/internal/database/kmskeys"
	"nautilus/internal/errors"
)

// RotateOrganization requests new backing material for the organization's KMS
// key. Its ARN, wrapped application key, and application ciphertext stay fixed.
func (m *Manager) RotateOrganization(ctx context.Context, orgID string) error {
	org, err := m.organization(ctx, orgID)
	if err != nil {
		return err
	}
	key, err := kmskeys.GetOrganization(ctx, m.db, org.ID)
	if err != nil {
		return err
	}
	return m.rotate(ctx, key)
}

// RotateUser requests backing material rotation for the shared user KMS key.
func (m *Manager) RotateUser(ctx context.Context) error {
	key, err := kmskeys.GetUser(ctx, m.db)
	if err != nil {
		return err
	}
	return m.rotate(ctx, key)
}

func (m *Manager) rotate(ctx context.Context, key *kmskeys.Key) error {
	if key == nil {
		return errors.New("kms: application key not provisioned")
	}
	if !keyARN.MatchString(key.ProviderKeyID) || len(key.Ciphertext) == 0 {
		return errors.New("kms: invalid application key record")
	}
	_, err := m.client.RotateKeyOnDemand(ctx, &kms.RotateKeyOnDemandInput{
		KeyId: &key.ProviderKeyID,
	}, func(o *kms.Options) {
		// Rotation has no idempotency token; a lost response must not trigger
		// another rotation. Check AWS rotation status before retrying manually.
		o.Retryer = aws.NopRetryer{}
	})
	if err != nil {
		return errors.Wrap(err, "kms: rotation request failed; check provider rotation status before retrying")
	}
	return nil
}
