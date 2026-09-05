package enums

type FeatureFlagObjectType string

const (
	FeatureFlagObjectTypeUser         FeatureFlagObjectType = "user"
	FeatureFlagObjectTypeOrganization FeatureFlagObjectType = "organization"
)

func (t FeatureFlagObjectType) String() string {
	return string(t)
}
