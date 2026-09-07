package awskms

import (
	"context"
	"regexp"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/kms"
	"github.com/aws/aws-sdk-go-v2/service/kms/types"

	"nautilus/internal/database"
	"nautilus/internal/database/kmskeys"
	"nautilus/internal/database/organizations"
	"nautilus/internal/errors"
)

var keyARN = regexp.MustCompile(`^arn:aws(?:-[a-z0-9-]+)?:kms:[a-z0-9-]+:[0-9]{12}:key/(?:[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}|mrk-[0-9a-f]{32})$`)

type Manager struct {
	client *kms.Client
	db     database.Database
}

func New(cfg aws.Config, db database.Database) *Manager {
	return &Manager{
		client: kms.NewFromConfig(cfg, func(o *kms.Options) {
			// Decrypt responses contain plaintext secrets.
			o.ClientLogMode &^= aws.LogRequestWithBody | aws.LogResponseWithBody
		}),
		db: db,
	}
}

func (m *Manager) OrganizationKey(ctx context.Context, orgID string) ([]byte, error) {
	org, err := m.organization(ctx, orgID)
	if err != nil {
		return nil, err
	}
	key, err := kmskeys.GetOrganization(ctx, m.db, org.ID)
	if err != nil {
		return nil, err
	}
	return m.decrypt(ctx, key, organizationContext(org.ExternalID))
}

func (m *Manager) UserKey(ctx context.Context) ([]byte, error) {
	key, err := kmskeys.GetUser(ctx, m.db)
	if err != nil {
		return nil, err
	}
	return m.decrypt(ctx, key, userContext())
}

// ProvisionOrganization binds an existing KMS key to the organization. It never
// replaces a binding or creates a provider-managed key.
func (m *Manager) ProvisionOrganization(ctx context.Context, orgID string, arn string) error {
	if !keyARN.MatchString(arn) {
		return errors.New("kms: canonical key ARN required")
	}
	org, err := m.organization(ctx, orgID)
	if err != nil {
		return err
	}
	key, err := kmskeys.GetOrganization(ctx, m.db, org.ID)
	if err != nil {
		return err
	}
	if key != nil {
		return checkBinding(key, arn)
	}
	blob, err := m.generate(ctx, arn, organizationContext(org.ExternalID))
	if err != nil {
		return err
	}
	key, err = kmskeys.CreateOrganization(ctx, m.db, org.ID, arn, blob)
	if err != nil {
		return err
	}
	return checkBinding(key, arn)
}

func (m *Manager) ProvisionUser(ctx context.Context, arn string) error {
	if !keyARN.MatchString(arn) {
		return errors.New("kms: canonical key ARN required")
	}
	key, err := kmskeys.GetUser(ctx, m.db)
	if err != nil {
		return err
	}
	if key != nil {
		return checkBinding(key, arn)
	}
	blob, err := m.generate(ctx, arn, userContext())
	if err != nil {
		return err
	}
	key, err = kmskeys.CreateUser(ctx, m.db, arn, blob)
	if err != nil {
		return err
	}
	return checkBinding(key, arn)
}

func (m *Manager) organization(ctx context.Context, orgID string) (*organizations.Organization, error) {
	org, err := organizations.GetByExternalID(ctx, m.db, orgID)
	if err != nil {
		return nil, err
	}
	if org == nil {
		return nil, errors.New("kms: organization not found")
	}
	return org, nil
}

func (m *Manager) generate(ctx context.Context, arn string, binding map[string]string) ([]byte, error) {
	out, err := m.client.GenerateDataKeyWithoutPlaintext(ctx, &kms.GenerateDataKeyWithoutPlaintextInput{
		KeyId: &arn, KeySpec: types.DataKeySpecAes256, EncryptionContext: binding,
	})
	if err != nil {
		return nil, errors.Wrap(err, "kms: unable to generate application key")
	}
	if aws.ToString(out.KeyId) != arn || len(out.CiphertextBlob) == 0 {
		return nil, errors.New("kms: invalid generated key response")
	}
	return out.CiphertextBlob, nil
}

func (m *Manager) decrypt(ctx context.Context, key *kmskeys.Key, binding map[string]string) ([]byte, error) {
	if key == nil {
		return nil, errors.New("kms: application key not provisioned")
	}
	if !keyARN.MatchString(key.ProviderKeyID) || len(key.Ciphertext) == 0 {
		return nil, errors.New("kms: invalid application key record")
	}
	out, err := m.client.Decrypt(ctx, &kms.DecryptInput{
		KeyId: &key.ProviderKeyID, CiphertextBlob: key.Ciphertext, EncryptionContext: binding,
		EncryptionAlgorithm: types.EncryptionAlgorithmSpecSymmetricDefault,
	})
	if err != nil {
		return nil, errors.Wrap(err, "kms: unable to decrypt application key")
	}
	if aws.ToString(out.KeyId) != key.ProviderKeyID || len(out.Plaintext) != 32 {
		clear(out.Plaintext)
		return nil, errors.New("kms: invalid decrypted key response")
	}
	return out.Plaintext, nil
}

func checkBinding(key *kmskeys.Key, arn string) error {
	if key == nil {
		return errors.New("kms: application key not provisioned")
	}
	if key.ProviderKeyID != arn {
		return errors.New("kms: application key already bound to a different KMS key")
	}
	return nil
}

func organizationContext(orgID string) map[string]string {
	return map[string]string{"application": "nautilus", "format": "1", "scope": "organization", "organization_id": orgID}
}

func userContext() map[string]string {
	return map[string]string{"application": "nautilus", "format": "1", "scope": "users"}
}
