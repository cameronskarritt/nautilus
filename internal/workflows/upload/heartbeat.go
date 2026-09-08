package upload

import (
	"context"
	"time"

	"go.temporal.io/sdk/activity"

	"nautilus/internal/temporal"
)

func heartbeat(ctx context.Context) (context.Context, func()) {
	if !activity.IsActivity(ctx) {
		return ctx, func() {}
	}
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer cancel()
		var err error
		defer temporal.Recover(ctx, &err)
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		stop := activity.GetWorkerStopChannel(ctx)
		for {
			if ctx.Err() != nil {
				return
			}
			// No document data belongs in heartbeat details or Temporal history.
			activity.RecordHeartbeat(ctx)
			select {
			case <-ctx.Done():
				return
			case <-stop:
				return
			case <-ticker.C:
			}
		}
	}()
	return ctx, func() { cancel(); <-done }
}
