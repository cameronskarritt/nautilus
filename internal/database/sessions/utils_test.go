package sessions_test

import (
	"testing"

	"nautilus/internal/database/sessions"
	"nautilus/internal/testutil/require"
)

func TestDeleteCookie(t *testing.T) {
	t.Parallel()

	created := sessions.CreateCookie("session-token")
	deleted := sessions.DeleteCookie()
	require.Equal(t, created.Name, deleted.Name)
	require.Equal(t, created.Path, deleted.Path)
	require.Equal(t, created.Domain, deleted.Domain)
	require.Equal(t, created.Secure, deleted.Secure)
	require.Equal(t, created.HttpOnly, deleted.HttpOnly)
	require.Empty(t, deleted.Value)
	require.Equal(t, -1, deleted.MaxAge)
}
