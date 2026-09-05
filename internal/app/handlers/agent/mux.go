package agent

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"nautilus/internal/ai/agent"
	"nautilus/internal/ai/agent/signalintake"
	"nautilus/internal/ai/eventlog"
	"nautilus/internal/ai/eventlog/pgeventlog"
	"nautilus/internal/database"
	"nautilus/internal/database/agentapprovals"
	"nautilus/internal/database/agentstreams"
	"nautilus/internal/database/organizations"
	"nautilus/internal/database/users"
	"nautilus/internal/errors"
	"nautilus/internal/httputil"
	"nautilus/internal/log"
	"nautilus/internal/mux"
	"nautilus/internal/pagination"
	"nautilus/internal/pubsub"
)

type Mux struct {
	db       database.Database
	eventLog eventlog.EventLog
	intake   *signalintake.Intake
	pubsub   pubsub.PubSub
}

func NewMux(db database.Database, pubsubClient pubsub.PubSub) *Mux {
	eventLog := pgeventlog.New(db)
	return &Mux{
		db:       db,
		eventLog: eventLog,
		intake:   signalintake.New(db),
		pubsub:   pubsubClient,
	}
}

func (m *Mux) Mount(r *mux.Router, prefix string) {
	sub := r.SubRouter(prefix)

	sub.Get("/streams", m.ListStreams)
	sub.Post("/streams", m.CreateStream)
	sub.Post("/streams/{streamID:<uuid>}/messages", m.SendMessage)
	sub.Post("/streams/{streamID:<uuid>}/stop", m.StopStream)

	sub.Get("/streams/{streamID:<uuid>}/events", m.StreamEvents)
	sub.Get("/streams/{streamID:<uuid>}/events/history", m.GetEventHistory)

	sub.Post("/approvals/{approvalID:<uuid>}/resolve", m.ResolveApproval)
	sub.Get("/approvals", m.ListApprovals)
}

// ListStreams returns a page of agent streams ordered by most recently updated,
// each with a title preview derived from the first user message.
//
// Supports `limit` (max 50) and `cursor` query parameters for keyset pagination.
func (m *Mux) ListStreams(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	org := organizations.FromContext(ctx)
	if org == nil {
		httputil.Error(ctx, w, ErrOrganizationRequired)
		return
	}

	params, err := pagination.ParseParams(r, 50)
	if err != nil {
		httputil.Error(ctx, w, err)
		return
	}

	page, err := agentstreams.List(ctx, m.db, org.ID, params)
	if err != nil {
		httputil.Error(ctx, w, err)
		return
	}

	httputil.JSON(ctx, w, page)
}

func (m *Mux) CreateStream(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	logger := log.FromContext(ctx)

	user := users.FromContext(ctx)
	org := organizations.FromContext(ctx)
	if user == nil || org == nil {
		httputil.Error(ctx, w, ErrOrganizationRequired)
		return
	}

	var form CreateStreamForm
	if err := httputil.ProcessForm(r, &form); err != nil {
		httputil.Error(ctx, w, err)
		return
	}

	accepted, err := m.intake.CreateStream(ctx, user.ID, org.ID, form.Message)
	if err != nil {
		httputil.Error(ctx, w, err)
		return
	}

	logger.Info("accepted agent stream", "stream_id", accepted.Stream.ExternalID, "signal_id", accepted.ID)

	httputil.JSON(ctx, w, map[string]any{
		"stream_id": accepted.Stream.ExternalID,
		"status":    accepted.Stream.Status,
	}, http.StatusCreated)
}

func (m *Mux) SendMessage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	logger := log.FromContext(ctx)

	streamID, _ := mux.PathParam(r, "streamID")
	org := organizations.FromContext(ctx)
	if org == nil {
		httputil.Error(ctx, w, ErrOrganizationRequired)
		return
	}

	var form SendMessageForm
	if err := httputil.ProcessForm(r, &form); err != nil {
		httputil.Error(ctx, w, err)
		return
	}

	accepted, err := m.intake.Message(ctx, org.ID, streamID, form.Message)
	if err != nil {
		httputil.Error(ctx, w, err)
		return
	}
	if accepted == nil {
		httputil.Error(ctx, w, ErrStreamNotFound)
		return
	}

	logger.Info("accepted agent message", "stream_id", streamID, "signal_id", accepted.ID)

	httputil.JSON(ctx, w, map[string]any{
		"stream_id":  streamID,
		"message_id": accepted.ID,
	}, http.StatusAccepted)
}

func (m *Mux) StopStream(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	logger := log.FromContext(ctx)

	streamID, _ := mux.PathParam(r, "streamID")
	org := organizations.FromContext(ctx)
	if org == nil {
		httputil.Error(ctx, w, ErrOrganizationRequired)
		return
	}

	accepted, err := m.intake.Stop(ctx, org.ID, streamID)
	if err != nil {
		httputil.Error(ctx, w, err)
		return
	}
	if accepted == nil {
		httputil.Error(ctx, w, ErrStreamNotFound)
		return
	}

	logger.Info("accepted agent stop", "stream_id", streamID, "signal_id", accepted.ID)

	httputil.JSON(ctx, w, map[string]any{
		"stream_id":  streamID,
		"message_id": accepted.ID,
	}, http.StatusAccepted)
}

// StreamEvents streams events for a stream via SSE (with catch-up from EventLog + live from PubSub).
func (m *Mux) StreamEvents(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	logger := log.FromContext(ctx)

	streamID, _ := mux.PathParam(r, "streamID")
	org := organizations.FromContext(ctx)
	if org == nil {
		httputil.Error(ctx, w, ErrOrganizationRequired)
		return
	}

	afterSeq := int64(0)
	if after := r.URL.Query().Get("after_sequence"); after != "" {
		parsed, err := strconv.ParseInt(after, 10, 64)
		if err == nil {
			afterSeq = parsed
		}
	}

	stream, err := agentstreams.GetByExternalIDForOrganization(ctx, m.db, org.ID, streamID)
	if err != nil {
		httputil.Error(ctx, w, err)
		return
	}
	if stream == nil {
		httputil.Error(ctx, w, ErrStreamNotFound)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		httputil.Error(ctx, w, ErrStreamingUnavailable)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	logger.Info("streaming events", "stream_id", streamID, "after_sequence", afterSeq)

	// Subscribe before catch-up so live tokens published during replay are not missed.
	topic := fmt.Sprintf("agent:%s", streamID)
	subscription, err := m.pubsub.Subscribe(ctx, topic)
	if err != nil {
		logger.Error("failed to subscribe to agent events", "stream_id", streamID, "error", err)
		writeSSEError(w, flusher)
		return
	}
	defer func() {
		if e := subscription.Close(); e != nil {
			logger.Error("failed to unsubscribe from agent topic", "topic", topic, "error", e)
		}
	}()

	events, err := m.eventLog.List(ctx, streamID, afterSeq)
	if err != nil {
		logger.Error("failed to list agent events", "stream_id", streamID, "error", err)
		writeSSEError(w, flusher)
		return
	}

	for _, event := range events {
		writeSSEEvent(w, flusher, event)
	}

	for {
		select {
		case <-ctx.Done():
			return

		case data, ok := <-subscription.Messages():
			if !ok {
				return
			}

			var token map[string]any
			if err := json.Unmarshal(data, &token); err != nil {
				logger.Error("failed to unmarshal token", "error", err)
				continue
			}

			writeSSEJSON(w, flusher, "token", token)
		}
	}
}

func (m *Mux) GetEventHistory(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	streamID, _ := mux.PathParam(r, "streamID")
	org := organizations.FromContext(ctx)
	if org == nil {
		httputil.Error(ctx, w, ErrOrganizationRequired)
		return
	}

	stream, err := agentstreams.GetByExternalIDForOrganization(ctx, m.db, org.ID, streamID)
	if err != nil {
		httputil.Error(ctx, w, err)
		return
	}
	if stream == nil {
		httputil.Error(ctx, w, ErrStreamNotFound)
		return
	}

	events, err := m.eventLog.List(ctx, streamID, 0)
	if err != nil {
		httputil.Error(ctx, w, err)
		return
	}

	httputil.JSON(ctx, w, map[string]any{
		"stream_id": streamID,
		"events":    events,
	})
}

// ResolveApproval resolves a pending approval by approving or rejecting it,
// then publishes a signal.approval event to the agent queue.
func (m *Mux) ResolveApproval(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	logger := log.FromContext(ctx)
	user := users.FromContext(ctx)
	org := organizations.FromContext(ctx)
	if user == nil || org == nil {
		httputil.Error(ctx, w, ErrOrganizationRequired)
		return
	}

	approvalID, _ := mux.PathParam(r, "approvalID")

	var form ResolveApprovalForm
	if err := httputil.ProcessForm(r, &form); err != nil {
		httputil.Error(ctx, w, err)
		return
	}

	updated, err := m.intake.ResolveApproval(ctx, signalintake.ApprovalResolution{
		OrganizationID: org.ID,
		ApprovalID:     approvalID,
		Approved:       form.Approved,
		Reason:         form.Reason,
		ApproverID:     user.ID,
		Approver: agent.Approver{
			Username: user.Username.Or(""),
			Email:    user.Email.Or(""),
		},
		ApproverMessage: form.Message,
	})
	if err != nil {
		if errors.Is(err, agentapprovals.ErrNotPending) {
			httputil.Error(ctx, w, ErrApprovalNotPending)
			return
		}
		httputil.Error(ctx, w, err)
		return
	}
	if updated == nil {
		httputil.Error(ctx, w, ErrApprovalNotFound)
		return
	}

	logger.Info("accepted approval signal", "approval_id", approvalID, "approved", form.Approved)

	httputil.JSON(ctx, w, map[string]any{
		"approval": updated,
	}, http.StatusAccepted)
}

func (m *Mux) ListApprovals(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	org := organizations.FromContext(ctx)
	if org == nil {
		httputil.Error(ctx, w, ErrOrganizationRequired)
		return
	}

	approvals, err := agentapprovals.ListPending(ctx, m.db, org.ID)
	if err != nil {
		httputil.Error(ctx, w, err)
		return
	}

	if approvals == nil {
		approvals = []*agentapprovals.Approval{}
	}

	httputil.JSON(ctx, w, map[string]any{
		"approvals": approvals,
	})
}

func writeSSEEvent(w http.ResponseWriter, flusher http.Flusher, event *eventlog.Event) {
	data, err := json.Marshal(event)
	if err != nil {
		return
	}
	fmt.Fprintf(w, "event: event\ndata: %s\n\n", data)
	flusher.Flush()
}

func writeSSEJSON(w http.ResponseWriter, flusher http.Flusher, eventType string, data any) {
	raw, err := json.Marshal(data)
	if err != nil {
		return
	}
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", eventType, raw)
	flusher.Flush()
}

func writeSSEError(w http.ResponseWriter, flusher http.Flusher) {
	data := map[string]string{
		"error": "Unable to stream agent events",
		"code":  errors.ErrorCodeAGENT05,
	}
	raw, marshalErr := json.Marshal(data)
	if marshalErr != nil {
		raw = []byte(`{"error":"unable to serialize error payload"}`)
	}
	fmt.Fprintf(w, "event: error\ndata: %s\n\n", raw)
	flusher.Flush()
}
