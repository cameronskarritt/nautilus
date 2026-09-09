package documents_test

import (
	"testing"

	"nautilus/internal/database/documents"
	"nautilus/internal/enums"
	"nautilus/internal/testutil"
	"nautilus/internal/testutil/require"
)

func TestUploadRecoveryFence(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	org := createOrganization(t, db, "recovery")
	original, err := documents.Create(t.Context(), db, org.ID, &documents.CreateOptions{Filename: "scan.pdf", ContentType: "application/pdf"})
	require.NoError(t, err)
	claim, err := documents.ClaimUpload(t.Context(), db, 0)
	require.NoError(t, err)
	require.Nil(t, claim, "active uploads cannot be claimed")
	_, err = db.Exec(t.Context(), `UPDATE documents SET upload_expires_at = CURRENT_TIMESTAMP - INTERVAL '1 minute' WHERE id = $1`, original.ID)
	require.NoError(t, err)
	claim, err = documents.ClaimUpload(t.Context(), db, 0)
	require.NoError(t, err)
	require.NotNil(t, claim)
	require.NotEqual(t, original.UploadToken, claim.UploadToken)
	ready, err := documents.ReadyUpload(t.Context(), db, original)
	require.NoError(t, err)
	require.False(t, ready, "expired uploader cannot hand off after recovery claims it")
	require.NoError(t, documents.ReleaseUpload(t.Context(), db, original))
	require.NoError(t, documents.FailUpload(t.Context(), db, original))
	other, err := documents.ClaimUpload(t.Context(), db, 0)
	require.NoError(t, err)
	require.Nil(t, other, "stale release must not expire a newer claim")
	ready, err = documents.ReadyUpload(t.Context(), db, claim)
	require.NoError(t, err)
	require.True(t, ready)
	require.NoError(t, documents.FailUpload(t.Context(), db, claim))
	got, err := documents.GetByExternalID(t.Context(), db, org.ID, original.ExternalID)
	require.NoError(t, err)
	require.Equal(t, enums.DocumentStatusUploading, got.Status)
	require.True(t, got.UploadReady, "ready uploads cannot be failed as abandoned")
}

func TestUploadRecoveryCursorAndOwnership(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	org := createOrganization(t, db, "recovery-cursor")
	first, err := documents.Create(t.Context(), db, org.ID, &documents.CreateOptions{Filename: "first", ContentType: "text/plain"})
	require.NoError(t, err)
	second, err := documents.Create(t.Context(), db, org.ID, &documents.CreateOptions{Filename: "second", ContentType: "text/plain"})
	require.NoError(t, err)
	_, err = db.Exec(t.Context(), `UPDATE documents SET upload_expires_at = CURRENT_TIMESTAMP - INTERVAL '1 minute' WHERE organization_id = $1`, org.ID)
	require.NoError(t, err)
	claim, err := documents.ClaimUpload(t.Context(), db, first.ID)
	require.NoError(t, err)
	require.Equal(t, second.ID, claim.ID)
	wrong := *claim
	wrong.OrganizationID++
	ready, err := documents.ReadyUpload(t.Context(), db, &wrong)
	require.NoError(t, err)
	require.False(t, ready)
	require.NoError(t, documents.FailUpload(t.Context(), db, &wrong))
	require.NoError(t, documents.FailUpload(t.Context(), db, claim))
	got, err := documents.GetByExternalID(t.Context(), db, org.ID, second.ExternalID)
	require.NoError(t, err)
	require.Equal(t, enums.DocumentStatusFailed, got.Status)
	_, err = db.Exec(t.Context(), `UPDATE organizations SET deleted_at = CURRENT_TIMESTAMP WHERE id = $1`, org.ID)
	require.NoError(t, err)
	claim, err = documents.ClaimUpload(t.Context(), db, 0)
	require.NoError(t, err)
	require.Nil(t, claim)
}

func TestConcurrentUploadRecoveryClaims(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDBWithCommit(t)
	org := createOrganization(t, db, "recovery-concurrent")
	doc, err := documents.Create(t.Context(), db, org.ID, &documents.CreateOptions{Filename: "scan.pdf", ContentType: "application/pdf"})
	require.NoError(t, err)
	_, err = db.Exec(t.Context(), `UPDATE documents SET upload_expires_at = CURRENT_TIMESTAMP - INTERVAL '1 minute' WHERE id = $1`, doc.ID)
	require.NoError(t, err)
	type result struct {
		doc *documents.Document
		err error
	}
	results := make(chan result, 2)
	start := make(chan struct{})
	for range 2 {
		go func() { <-start; got, err := documents.ClaimUpload(t.Context(), db, 0); results <- result{got, err} }()
	}
	close(start)
	claims := 0
	for range 2 {
		result := <-results
		require.NoError(t, result.err)
		if result.doc != nil {
			claims++
		}
	}
	require.Equal(t, 1, claims)
}
