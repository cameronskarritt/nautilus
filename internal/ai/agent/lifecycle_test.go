package agent

import (
	"testing"

	"nautilus/internal/database/agentstreams"
	"nautilus/internal/testutil/require"
)

func TestNextLifecycleStatus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		status agentstreams.Status
		event  lifecycleEvent
		want   agentstreams.Status
		ok     bool
	}{
		{name: "running suspends", status: agentstreams.StatusRunning, event: lifecycleSuspend, want: agentstreams.StatusAwaitingApproval, ok: true},
		{name: "suspension is idempotent", status: agentstreams.StatusAwaitingApproval, event: lifecycleSuspend, want: agentstreams.StatusAwaitingApproval, ok: true},
		{name: "approval resumes", status: agentstreams.StatusAwaitingApproval, event: lifecycleResume, want: agentstreams.StatusRunning, ok: true},
		{name: "resume is idempotent", status: agentstreams.StatusRunning, event: lifecycleResume, want: agentstreams.StatusRunning, ok: true},
		{name: "running idles", status: agentstreams.StatusRunning, event: lifecycleIdle, want: agentstreams.StatusIdle, ok: true},
		{name: "running fails", status: agentstreams.StatusRunning, event: lifecycleFail, want: agentstreams.StatusFailed, ok: true},
		{name: "suspended fails", status: agentstreams.StatusAwaitingApproval, event: lifecycleFail, want: agentstreams.StatusFailed, ok: true},
		{name: "running cancels", status: agentstreams.StatusRunning, event: lifecycleCancel, want: agentstreams.StatusCancelled, ok: true},
		{name: "suspended cancels", status: agentstreams.StatusAwaitingApproval, event: lifecycleCancel, want: agentstreams.StatusCancelled, ok: true},
		{name: "idle cancels", status: agentstreams.StatusIdle, event: lifecycleCancel, want: agentstreams.StatusCancelled, ok: true},
		{name: "cancel is idempotent", status: agentstreams.StatusCancelled, event: lifecycleCancel, want: agentstreams.StatusCancelled, ok: true},
		{name: "idle cannot suspend", status: agentstreams.StatusIdle, event: lifecycleSuspend},
		{name: "suspended cannot idle", status: agentstreams.StatusAwaitingApproval, event: lifecycleIdle},
		{name: "pending cannot idle", status: agentstreams.StatusPending, event: lifecycleIdle},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, ok := nextLifecycleStatus(tt.status, tt.event)
			require.Equal(t, tt.ok, ok)
			require.Equal(t, tt.want, got)
		})
	}
}
