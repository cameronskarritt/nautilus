package agent

import (
	"context"

	"nautilus/internal/database"
	"nautilus/internal/database/agentstreams"
	"nautilus/internal/errors"
)

type lifecycleEvent string

const (
	lifecycleSuspend lifecycleEvent = "suspend"
	lifecycleResume  lifecycleEvent = "resume"
	lifecycleIdle    lifecycleEvent = "idle"
	lifecycleFail    lifecycleEvent = "fail"
	lifecycleCancel  lifecycleEvent = "cancel"
)

type lifecycle struct {
	db         database.Database
	streamID   int
	fenceToken int64
	status     agentstreams.Status
	pending    *PendingApproval
}

func newLifecycle(
	db database.Database,
	streamID int,
	fenceToken int64,
	status agentstreams.Status,
) *lifecycle {
	return &lifecycle{
		db:         db,
		streamID:   streamID,
		fenceToken: fenceToken,
		status:     status,
	}
}

func nextLifecycleStatus(status agentstreams.Status, event lifecycleEvent) (agentstreams.Status, bool) {
	switch event {
	case lifecycleSuspend:
		if status == agentstreams.StatusRunning || status == agentstreams.StatusAwaitingApproval {
			return agentstreams.StatusAwaitingApproval, true
		}
	case lifecycleResume:
		if status == agentstreams.StatusAwaitingApproval || status == agentstreams.StatusRunning {
			return agentstreams.StatusRunning, true
		}
	case lifecycleIdle:
		if status == agentstreams.StatusRunning {
			return agentstreams.StatusIdle, true
		}
	case lifecycleFail:
		if status == agentstreams.StatusRunning || status == agentstreams.StatusAwaitingApproval {
			return agentstreams.StatusFailed, true
		}
	case lifecycleCancel:
		return agentstreams.StatusCancelled, true
	}
	return "", false
}

func (l *lifecycle) transition(ctx context.Context, event lifecycleEvent) error {
	next, ok := nextLifecycleStatus(l.status, event)
	if !ok {
		return errors.Errorf("invalid agent lifecycle transition: %s -> %s", l.status, event)
	}
	if next == l.status {
		return nil
	}
	if err := agentstreams.ProjectStatus(ctx, l.db, l.streamID, l.fenceToken, next); err != nil {
		return err
	}
	l.status = next
	return nil
}

func (l *lifecycle) reconcile(ctx context.Context, pending *PendingApproval) (*PendingApproval, bool, error) {
	event := lifecycleResume
	if pending != nil {
		event = lifecycleSuspend
	}
	if err := l.transition(ctx, event); err != nil {
		return nil, false, err
	}

	requested := pending != nil &&
		(l.pending == nil || l.pending.ApprovalID != pending.ApprovalID)
	l.pending = pending
	return l.pending, requested, nil
}

func (l *lifecycle) canIdle() bool {
	return l.pending == nil
}
