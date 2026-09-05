package signalintake

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"time"

	"nautilus/internal/ai/agent"
	"nautilus/internal/ai/eventlog"
	"nautilus/internal/ai/eventlog/pgeventlog"
	"nautilus/internal/database"
	"nautilus/internal/database/agentapprovals"
	"nautilus/internal/database/agentstreams"
	"nautilus/internal/database/outboxevents"
	"nautilus/internal/enums"
	"nautilus/internal/errors"
	"nautilus/internal/optional"
)

type AcceptedSignal struct {
	ID     string
	Stream *agentstreams.Stream
}

type ApprovalResolution struct {
	OrganizationID  int
	ApprovalID      string
	Approved        bool
	Reason          string
	ApproverID      int
	Approver        agent.Approver
	ApproverMessage optional.Optional[string]
}

type Intake struct {
	db database.Database
}

func New(db database.Database) *Intake {
	return &Intake{db: db}
}

func (i *Intake) CreateStream(
	ctx context.Context,
	userID, organizationID int,
	message string,
) (*AcceptedSignal, error) {
	signalID, err := newSignalID()
	if err != nil {
		return nil, err
	}

	var stream *agentstreams.Stream
	err = database.Transact(ctx, i.db, func(tx database.Database) error {
		stream, err = agentstreams.Create(ctx, tx, userID, organizationID)
		if err != nil {
			return err
		}
		if err := agentstreams.SetTitle(ctx, tx, stream.ID, message); err != nil {
			return err
		}
		stream, err = agentstreams.Get(ctx, tx, stream.ID)
		if err != nil {
			return err
		}
		return acceptMessage(ctx, tx, stream, signalID, message)
	})
	if err != nil {
		return nil, err
	}
	return &AcceptedSignal{ID: signalID, Stream: stream}, nil
}

func (i *Intake) Message(
	ctx context.Context,
	organizationID int,
	streamID, content string,
) (*AcceptedSignal, error) {
	signalID, err := newSignalID()
	if err != nil {
		return nil, err
	}

	var stream *agentstreams.Stream
	err = database.Transact(ctx, i.db, func(tx database.Database) error {
		stream, err = agentstreams.GetByExternalIDForOrganization(ctx, tx, organizationID, streamID)
		if err != nil || stream == nil {
			return err
		}
		return acceptMessage(ctx, tx, stream, signalID, content)
	})
	if err != nil || stream == nil {
		return nil, err
	}
	return &AcceptedSignal{ID: signalID, Stream: stream}, nil
}

func (i *Intake) Stop(ctx context.Context, organizationID int, streamID string) (*AcceptedSignal, error) {
	signalID, err := newSignalID()
	if err != nil {
		return nil, err
	}

	var stream *agentstreams.Stream
	err = database.Transact(ctx, i.db, func(tx database.Database) error {
		stream, err = agentstreams.GetByExternalIDForOrganization(ctx, tx, organizationID, streamID)
		if err != nil || stream == nil {
			return err
		}
		return acceptSignal(
			ctx,
			tx,
			stream,
			signalID,
			enums.AgentEventSignalStop,
			nil,
		)
	})
	if err != nil || stream == nil {
		return nil, err
	}
	return &AcceptedSignal{ID: signalID, Stream: stream}, nil
}

func (i *Intake) ResolveApproval(
	ctx context.Context,
	resolution ApprovalResolution,
) (*agentapprovals.Approval, error) {
	var updated *agentapprovals.Approval
	err := database.Transact(ctx, i.db, func(tx database.Database) error {
		approval, err := agentapprovals.GetByExternalIDForOrganization(
			ctx,
			tx,
			resolution.OrganizationID,
			resolution.ApprovalID,
		)
		if err != nil || approval == nil {
			return err
		}
		if approval.Status != agentapprovals.StatusPending {
			return agentapprovals.ErrNotPending
		}

		status := agentapprovals.StatusApproved
		if !resolution.Approved {
			status = agentapprovals.StatusRejected
		}
		reason := optional.Empty[string]()
		if resolution.Reason != "" {
			reason = optional.Set(resolution.Reason)
		}
		if err := agentapprovals.Resolve(
			ctx,
			tx,
			approval.ID,
			status,
			resolution.ApproverID,
			reason,
			resolution.ApproverMessage,
		); err != nil {
			return err
		}

		stream, err := agentstreams.Get(ctx, tx, approval.StreamID)
		if err != nil {
			return err
		}
		decision := agent.ApprovalDecision{
			ApprovalID: resolution.ApprovalID,
			Approved:   resolution.Approved,
			Reason:     resolution.Reason,
			Approver:   resolution.Approver,
		}
		if err := acceptSignal(
			ctx,
			tx,
			stream,
			resolution.ApprovalID,
			enums.AgentEventSignalApproval,
			decision,
		); err != nil {
			return err
		}

		updated, err = agentapprovals.GetByExternalIDForOrganization(
			ctx,
			tx,
			resolution.OrganizationID,
			resolution.ApprovalID,
		)
		return err
	})
	if err != nil || updated == nil {
		return nil, err
	}
	return updated, nil
}

func acceptMessage(
	ctx context.Context,
	db database.Database,
	stream *agentstreams.Stream,
	signalID, content string,
) error {
	message := agent.MessagePayload{ID: signalID, Content: content}
	tokens := eventlog.Tokens{Fence: stream.FenceToken, Idempotency: signalID}
	logger := pgeventlog.New(db)

	if _, err := logger.Append(
		ctx,
		stream.ExternalID,
		enums.AgentEventSignalReceived,
		enums.AgentEventSourceAPI,
		nil,
		tokens,
	); err != nil {
		return err
	}
	if _, err := logger.Append(
		ctx,
		stream.ExternalID,
		enums.AgentEventMessage,
		enums.AgentEventSourceAPI,
		message,
		tokens,
	); err != nil {
		return err
	}
	return enqueueSignal(
		ctx,
		db,
		stream,
		signalID,
		enums.AgentEventSignalReceived,
		message,
	)
}

func acceptSignal(
	ctx context.Context,
	db database.Database,
	stream *agentstreams.Stream,
	signalID string,
	eventType enums.AgentEventType,
	payload any,
) error {
	if stream == nil {
		return errors.New("agent stream not found")
	}
	if _, err := pgeventlog.New(db).Append(
		ctx,
		stream.ExternalID,
		eventType,
		enums.AgentEventSourceAPI,
		payload,
		eventlog.Tokens{Fence: stream.FenceToken, Idempotency: signalID},
	); err != nil {
		return err
	}
	return enqueueSignal(ctx, db, stream, signalID, eventType, payload)
}

func enqueueSignal(
	ctx context.Context,
	db database.Database,
	stream *agentstreams.Stream,
	signalID string,
	eventType enums.AgentEventType,
	payload any,
) error {
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return errors.Wrap(err, "unable to marshal agent signal")
	}
	_, err = outboxevents.Enqueue(
		ctx,
		db,
		stream.OrgID,
		string(enums.QueueAgentSignals),
		stream.ExternalID,
		signalID,
		agent.QueueEvent{
			StreamID: stream.ExternalID,
			Type:     eventType,
			Source:   enums.AgentEventSourceAPI,
			Payload:  payloadJSON,
		},
		time.Time{},
	)
	return err
}

func newSignalID() (string, error) {
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return "", errors.Wrap(err, "unable to generate signal id")
	}
	return hex.EncodeToString(id[:]), nil
}
