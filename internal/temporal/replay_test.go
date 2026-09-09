package temporal_test

import "testing"

// These placeholders do not validate replay compatibility. Before enabling them,
// capture representative histories (including old Upload versions), register the
// production workflows and failure converter with worker.NewWorkflowReplayer,
// and fail on any replay error. Keep document content and secrets out of fixtures.
func TestWorkflowHistoryReplay(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"Upload", "UploadRecovery", "WebhookDelivery", "WebhookTest", "WebhookReplay", "WebhookRetention", "Smoke"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			t.Skip("replay coverage deferred: recorded history fixtures and replay assertions are not implemented")
		})
	}
}
