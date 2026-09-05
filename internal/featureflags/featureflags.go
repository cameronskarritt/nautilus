package featureflags

import (
	"context"
	"maps"

	"nautilus/internal/enums"
)

type Flag string

type FeatureFlagger interface {
	IsEnabled(ctx context.Context, objectType enums.FeatureFlagObjectType, objectID int, featureFlag Flag) bool
	List(ctx context.Context, objectType enums.FeatureFlagObjectType, objectID int) (map[string]bool, error)
}

// MergeFlags merges organization flags with user flags, with user flags taking preference.
func MergeFlags(orgFlags, userFlags map[string]bool) map[string]bool {
	merged := make(map[string]bool, len(orgFlags)+len(userFlags))

	maps.Copy(merged, orgFlags)
	maps.Copy(merged, userFlags)

	return merged
}
