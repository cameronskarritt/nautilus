package testutil

import (
	"context"
	"fmt"
	"sync"

	"nautilus/internal/enums"
	"nautilus/internal/featureflags"
)

// TestFeatureFlagger is an in-memory implementation of featureflags.FeatureFlagger for testing.
type TestFeatureFlagger struct {
	mu    sync.RWMutex
	flags map[string]map[string]bool
}

// NewTestFeatureFlagger creates a new in-memory feature flagger for testing.
func NewTestFeatureFlagger() *TestFeatureFlagger {
	return &TestFeatureFlagger{
		flags: make(map[string]map[string]bool),
	}
}

// SetFlags sets the flags for a given object type and ID.
func (f *TestFeatureFlagger) SetFlags(objectType enums.FeatureFlagObjectType, objectID int, flags map[string]bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := f.key(objectType, objectID)
	f.flags[key] = flags
}

// IsEnabled returns whether a flag is enabled for the given object.
func (f *TestFeatureFlagger) IsEnabled(ctx context.Context, objectType enums.FeatureFlagObjectType, objectID int, flag featureflags.Flag) bool {
	f.mu.RLock()
	defer f.mu.RUnlock()
	key := f.key(objectType, objectID)
	if flags, ok := f.flags[key]; ok {
		return flags[string(flag)]
	}
	return false
}

// List returns all flags for the given object.
func (f *TestFeatureFlagger) List(ctx context.Context, objectType enums.FeatureFlagObjectType, objectID int) (map[string]bool, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	key := f.key(objectType, objectID)
	if flags, ok := f.flags[key]; ok {
		result := make(map[string]bool, len(flags))
		for k, v := range flags {
			result[k] = v
		}
		return result, nil
	}
	return make(map[string]bool), nil
}

func (f *TestFeatureFlagger) key(objectType enums.FeatureFlagObjectType, objectID int) string {
	return fmt.Sprintf("%s:%d", objectType, objectID)
}
