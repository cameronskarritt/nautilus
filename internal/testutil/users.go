package testutil

import (
	"context"
	"testing"

	"nautilus/internal/database"
	"nautilus/internal/database/organizations"
	"nautilus/internal/database/users"
	"nautilus/internal/optional"
)

// TestUserOptions configures test user creation.
type TestUserOptions struct {
	// Suffix appends to the username/email for creating multiple users in the same test.
	Suffix string
	// Admin sets the user as an admin.
	Admin bool
}

// CreateTestUser creates a test user with a unique username and email based on the test name.
// Returns the user ID.
func CreateTestUser(t *testing.T, db database.Database, opts *TestUserOptions) int {
	t.Helper()
	ctx := context.Background()
	name := t.Name()
	if opts != nil && opts.Suffix != "" {
		name = name + "_" + opts.Suffix
	}
	user, err := users.Register(ctx, db,
		optional.Set("testuser_"+name),
		optional.Set(name+"@example.com"),
		"password123",
	)
	if err != nil {
		t.Fatalf("failed to create test user: %v", err)
	}

	if opts != nil && opts.Admin {
		err = users.SetAdmin(ctx, db, user.ID, true)
		if err != nil {
			t.Fatalf("failed to set user as admin: %v", err)
		}
	}

	return user.ID
}

// CreateTestOrgMember creates a test org member for a user in an organization.
// Returns the org member ID.
func CreateTestOrgMember(t *testing.T, db database.Database, userID int, orgID int, role organizations.Role) int {
	t.Helper()
	ctx := context.Background()

	member, err := organizations.CreateMember(ctx, db, userID, orgID, role, optional.Empty[string]())
	if err != nil {
		t.Fatalf("failed to create test org member: %v", err)
	}

	return member.ID
}

// CreateTestOrg creates a test organization.
// Returns the organization ID.
func CreateTestOrg(t *testing.T, db database.Database, slug string, name string) int {
	t.Helper()
	ctx := context.Background()

	org, err := organizations.Create(ctx, db, slug, name, false, optional.Empty[organizations.Settings]())
	if err != nil {
		t.Fatalf("failed to create test organization: %v", err)
	}

	return org.ID
}
