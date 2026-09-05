package httputil

import (
	"bytes"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"nautilus/internal/errors"
	"nautilus/internal/testutil"
)

// sensitiveHeaders are headers that should be redacted when recording.
var sensitiveHeaders = []string{
	"Authorization",
	"X-Api-Key",
	"Api-Key",
	"Cookie",
	"Set-Cookie",
	"OpenAI-Organization",
	"OpenAI-Project",
	"Anthropic-Organization-Id",
	"X-Request-Id",
	"Request-Id",
	"Cf-Ray",
}

// redactHeaders returns a copy of the headers with sensitive values redacted.
func redactHeaders(h http.Header) http.Header {
	if h == nil {
		return nil
	}
	redacted := h.Clone()
	for _, key := range sensitiveHeaders {
		if redacted.Get(key) != "" {
			redacted.Set(key, "[REDACTED]")
		}
	}
	return redacted
}

// RecordingTransport is an http.RoundTripper that records HTTP interactions
// to a cassette for later replay.
type RecordingTransport struct {
	Base     http.RoundTripper
	Cassette *testutil.Cassette
}

// NewRecordingTransport creates a new recording transport that wraps the
// given base transport and records interactions to the cassette.
func NewRecordingTransport(base http.RoundTripper, cassette *testutil.Cassette) *RecordingTransport {
	if base == nil {
		base = http.DefaultTransport
	}
	return &RecordingTransport{
		Base:     base,
		Cassette: cassette,
	}
}

// RoundTrip implements http.RoundTripper. It records the request and response
// to the cassette.
func (t *RecordingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Capture request body
	var reqBody []byte
	if req.Body != nil {
		var err error
		reqBody, err = io.ReadAll(req.Body)
		if err != nil {
			return nil, errors.Wrap(err, "failed to read request body")
		}
		req.Body.Close()
		req.Body = io.NopCloser(bytes.NewReader(reqBody))
	}

	recordedReq := testutil.RecordedRequest{
		Method: req.Method,
		URL:    req.URL.String(),
		Header: redactHeaders(req.Header),
		Body:   reqBody,
	}

	// Make the actual request
	resp, err := t.Base.RoundTrip(req)
	if err != nil {
		// Record the error case
		interaction := testutil.Interaction{
			Request: recordedReq,
			Response: testutil.RecordedResponse{
				Status: 0, // indicates error
			},
		}
		if err := t.Cassette.Append(interaction); err != nil {
			return nil, err
		}
		return nil, errors.Wrap(err, "error making request")
	}

	// Check if this is an SSE response
	contentType := resp.Header.Get("Content-Type")
	isSSE := strings.Contains(contentType, "text/event-stream")

	if isSSE {
		// Write stream start entry
		streamResp := testutil.RecordedResponse{
			Status: resp.StatusCode,
			Header: redactHeaders(resp.Header),
		}
		if err := t.Cassette.AppendStreamStart(recordedReq, streamResp); err != nil {
			return nil, err
		}

		// Wrap the body with a recording reader for SSE
		recorder := newSSERecordingReader(resp.Body, t.Cassette)
		resp.Body = recorder
	} else {
		// For non-streaming responses, read the entire body
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, errors.Wrap(err, "failed to read response body")
		}
		resp.Body.Close()

		interaction := testutil.Interaction{
			Request: recordedReq,
			Response: testutil.RecordedResponse{
				Status: resp.StatusCode,
				Header: redactHeaders(resp.Header),
				Body:   body,
			},
		}
		if err := t.Cassette.Append(interaction); err != nil {
			return nil, err
		}

		// Restore the body for the caller
		resp.Body = io.NopCloser(bytes.NewReader(body))
	}

	return resp, nil
}

// sseRecordingReader wraps a response body and records SSE events with timing.
type sseRecordingReader struct {
	reader    io.ReadCloser
	cassette  *testutil.Cassette
	startTime time.Time
}

func newSSERecordingReader(body io.ReadCloser, cassette *testutil.Cassette) *sseRecordingReader {
	return &sseRecordingReader{
		reader:    body,
		cassette:  cassette,
		startTime: time.Now(),
	}
}

func (r *sseRecordingReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)

	if n > 0 {
		offset := time.Since(r.startTime)
		// Write each chunk as an individual SSE event entry
		if appendErr := r.cassette.AppendSSEEvent(offset, string(p[:n])); appendErr != nil {
			return n, appendErr
		}
	}

	if err == nil {
		return n, nil
	}
	// Don't wrap io.EOF - it's a sentinel that callers check for.
	if err == io.EOF {
		return n, io.EOF
	}
	return n, errors.Wrap(err, "error reading SSE event")
}

func (r *sseRecordingReader) Close() error {
	err := r.reader.Close()
	if err != nil {
		return errors.Wrap(err, "error closing SSE reader")
	}
	return nil
}

// ReplayTransport is an http.RoundTripper that replays recorded HTTP
// interactions from a cassette.
type ReplayTransport struct {
	Cassette    *testutil.Cassette
	FastForward bool // Skip timing delays when replaying SSE events
	Consume     bool // Remove matched interactions (for multiple calls to same endpoint)
}

// ReplayOption configures the ReplayTransport.
type ReplayOption func(*ReplayTransport)

// WithFastForward configures the transport to skip timing delays during replay.
func WithFastForward() ReplayOption {
	return func(t *ReplayTransport) {
		t.FastForward = true
	}
}

// WithConsume configures the transport to remove matched interactions,
// allowing multiple calls to the same endpoint with different responses.
func WithConsume() ReplayOption {
	return func(t *ReplayTransport) {
		t.Consume = true
	}
}

// NewReplayTransport creates a new replay transport that serves responses
// from the given cassette.
func NewReplayTransport(cassette *testutil.Cassette, opts ...ReplayOption) *ReplayTransport {
	t := &ReplayTransport{
		Cassette: cassette,
	}
	for _, opt := range opts {
		opt(t)
	}
	return t
}

// ErrNoMatchingInteraction is returned when no recorded interaction matches
// the request.
var ErrNoMatchingInteraction = errors.New("no matching interaction found in cassette")

// RoundTrip implements http.RoundTripper. It finds a matching recorded
// interaction and returns the recorded response.
func (t *ReplayTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	var interaction *testutil.Interaction
	if t.Consume {
		interaction = t.Cassette.MatchAndConsume(req)
	} else {
		interaction = t.Cassette.Match(req)
	}

	if interaction == nil {
		return nil, errors.Wrapf(ErrNoMatchingInteraction, "%s %s", req.Method, req.URL)
	}

	recorded := &interaction.Response

	// Check if this was an error response
	if recorded.Status == 0 {
		return nil, errors.New("recorded interaction was an error")
	}

	// Build the response
	resp := &http.Response{
		StatusCode: recorded.Status,
		Status:     http.StatusText(recorded.Status),
		Header:     recorded.Header.Clone(),
		Request:    req,
	}

	// Handle SSE vs regular responses
	if len(recorded.Events) > 0 {
		// SSE response - use replay reader
		resp.Body = newSSEReplayReader(recorded.Events, t.FastForward)
		if resp.Header == nil {
			resp.Header = make(http.Header)
		}
		resp.Header.Set("Content-Type", "text/event-stream")
	} else {
		// Regular response
		resp.Body = io.NopCloser(bytes.NewReader(recorded.Body))
		resp.ContentLength = int64(len(recorded.Body))
	}

	return resp, nil
}

// sseReplayReader replays SSE events with optional timing delays.
type sseReplayReader struct {
	events      []testutil.SSEEvent
	fastForward bool
	startTime   time.Time
	eventIndex  int
	buffer      *bytes.Reader
	mu          sync.Mutex
}

func newSSEReplayReader(events []testutil.SSEEvent, fastForward bool) *sseReplayReader {
	return &sseReplayReader{
		events:      events,
		fastForward: fastForward,
		startTime:   time.Now(),
	}
}

func (r *sseReplayReader) Read(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// If we have buffered data from a previous event, return it first
	if r.buffer != nil && r.buffer.Len() > 0 {
		n, err := r.buffer.Read(p)
		if err != nil {
			return n, errors.Wrap(err, "error reading SSE event")
		}
		return n, nil
	}

	// Check if we have more events
	if r.eventIndex >= len(r.events) {
		return 0, io.EOF
	}

	event := r.events[r.eventIndex]
	r.eventIndex++

	// Apply timing delay if not fast-forwarding
	if !r.fastForward && event.Offset > 0 {
		elapsed := time.Since(r.startTime)
		if event.Offset > elapsed {
			r.mu.Unlock()
			time.Sleep(event.Offset - elapsed)
			r.mu.Lock()
		}
	}

	// Buffer the event data
	r.buffer = bytes.NewReader([]byte(event.Data))
	n, err := r.buffer.Read(p)
	if err != nil {
		return n, errors.Wrap(err, "error reading SSE event")
	}
	return n, nil
}

func (r *sseReplayReader) Close() error {
	return nil
}
