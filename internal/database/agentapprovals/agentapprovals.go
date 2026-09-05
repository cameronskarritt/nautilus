package agentapprovals

import (
	"context"
	"database/sql"
	"encoding/json"

	"nautilus/internal/ai/llm"
	"nautilus/internal/database"
	"nautilus/internal/database/agentstreams"
	"nautilus/internal/errors"
	"nautilus/internal/optional"
)

var ErrNotPending = errors.New("agent approval is not pending")

func Create(
	ctx context.Context,
	db database.Database,
	streamID int,
	fenceToken int64,
	toolCalls []llm.ToolCall,
) (*Approval, error) {
	toolCallsJSON, err := json.Marshal(toolCalls)
	if err != nil {
		return nil, errors.Wrap(err, "unable to marshal tool calls")
	}

	query := `
		INSERT INTO agent_approvals(stream_id, tool_calls)
		SELECT id, $2
		FROM agent_streams
		WHERE id = $1 AND fence_token = $3
		RETURNING id;
	`

	var id int
	err = db.QueryRow(ctx, query, streamID, toolCallsJSON, fenceToken).Scan(&id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, agentstreams.ErrFenceViolation
		}
		return nil, errors.Wrap(err, "unable to create agent approval")
	}

	return Get(ctx, db, id)
}

func Get(ctx context.Context, db database.Database, id int) (*Approval, error) {
	approval := new(Approval)

	query := `
		SELECT id, external_id, stream_id, status, tool_calls,
		       reason, approved_by, approver_message,
		       requested_at, resolved_at, created_at, updated_at
		FROM agent_approvals
		WHERE id = $1;
	`

	var toolCallsJSON []byte
	err := db.QueryRow(ctx, query, id).Scan(
		&approval.ID,
		&approval.ExternalID,
		&approval.StreamID,
		&approval.Status,
		&toolCallsJSON,
		&approval.Reason,
		&approval.ApprovedBy,
		&approval.ApproverMessage,
		&approval.RequestedAt,
		&approval.ResolvedAt,
		&approval.CreatedAt,
		&approval.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, errors.Wrap(err, "unable to fetch agent approval")
	}

	if err := json.Unmarshal(toolCallsJSON, &approval.ToolCalls); err != nil {
		return nil, errors.Wrap(err, "unable to unmarshal tool calls")
	}

	return approval, nil
}

func GetByExternalID(ctx context.Context, db database.Database, externalID string) (*Approval, error) {
	query := `SELECT id FROM agent_approvals WHERE external_id = $1;`

	var id int
	err := db.QueryRow(ctx, query, externalID).Scan(&id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, errors.Wrap(err, "unable to fetch agent approval by external ID")
	}

	return Get(ctx, db, id)
}

func GetByExternalIDForOrganization(
	ctx context.Context,
	db database.Database,
	organizationID int,
	externalID string,
) (*Approval, error) {
	query := `
		SELECT approval.id
		FROM agent_approvals approval
		JOIN agent_streams stream ON stream.id = approval.stream_id
		WHERE stream.org_id = $1 AND approval.external_id = $2;
	`
	var id int
	err := db.QueryRow(ctx, query, organizationID, externalID).Scan(&id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, errors.Wrap(err, "unable to fetch agent approval for organization")
	}
	return Get(ctx, db, id)
}

func Resolve(
	ctx context.Context,
	db database.Database,
	id int,
	status string,
	approvedBy int,
	reason optional.Optional[string],
	approverMessage optional.Optional[string],
) error {
	query := `
		UPDATE agent_approvals
		SET status = $1, approved_by = $2, reason = $3, approver_message = $4,
		    resolved_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP
		WHERE id = $5 AND status = 'pending';
	`

	result, err := db.Exec(ctx, query, status, approvedBy, reason, approverMessage, id)
	if err != nil {
		return errors.Wrap(err, "unable to resolve agent approval")
	}
	if result.RowsAffected() == 0 {
		return ErrNotPending
	}

	return nil
}

func GetPendingByStreamID(ctx context.Context, db database.Database, streamID int) (*Approval, error) {
	query := `SELECT id FROM agent_approvals WHERE stream_id = $1 AND status = $2;`

	var id int
	err := db.QueryRow(ctx, query, streamID, StatusPending).Scan(&id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, errors.Wrap(err, "unable to fetch pending approval for stream")
	}

	return Get(ctx, db, id)
}

func ListPending(ctx context.Context, db database.Database, organizationID int) ([]*Approval, error) {
	query := `
		SELECT approval.id, approval.external_id, approval.stream_id, approval.status,
		       approval.tool_calls, approval.reason, approval.approved_by,
		       approval.approver_message, approval.requested_at, approval.resolved_at,
		       approval.created_at, approval.updated_at
		FROM agent_approvals approval
		JOIN agent_streams stream ON stream.id = approval.stream_id
		WHERE stream.org_id = $1 AND approval.status = $2
		ORDER BY approval.requested_at ASC;
	`

	rows, err := db.Query(ctx, query, organizationID, StatusPending)
	if err != nil {
		return nil, errors.Wrap(err, "unable to list pending approvals")
	}

	var approvals []*Approval
	err = database.ScanRows(rows, func(row database.Row) error {
		a := new(Approval)
		var toolCallsJSON []byte
		if err := row.Scan(
			&a.ID,
			&a.ExternalID,
			&a.StreamID,
			&a.Status,
			&toolCallsJSON,
			&a.Reason,
			&a.ApprovedBy,
			&a.ApproverMessage,
			&a.RequestedAt,
			&a.ResolvedAt,
			&a.CreatedAt,
			&a.UpdatedAt,
		); err != nil {
			return errors.Wrap(err, "unable to scan agent approval")
		}

		if err := json.Unmarshal(toolCallsJSON, &a.ToolCalls); err != nil {
			return errors.Wrap(err, "unable to unmarshal tool calls")
		}

		approvals = append(approvals, a)
		return nil
	})
	if err != nil {
		return nil, err
	}

	return approvals, nil
}
