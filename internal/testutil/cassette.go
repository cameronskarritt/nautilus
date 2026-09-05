package testutil

import (
	"bufio"
	"encoding/json"
	"net/http"
	"os"
	"sync"
	"time"

	"nautilus/internal/errors"
)

// Entry types for JSONL format
const (
	EntryTypeInteraction = "interaction" // non-streaming request/response
	EntryTypeStreamStart = "stream"      // streaming request with response metadata
	EntryTypeSSEEvent    = "event"       // individual SSE event
)

// Entry is a single line in the JSONL cassette file.
type Entry struct {
	Type string `json:"type"`

	// For "interaction" and "stream" types
	Request  *RecordedRequest  `json:"request,omitempty"`
	Response *RecordedResponse `json:"response,omitempty"`

	// For "event" type
	Offset *time.Duration `json:"offset,omitempty"` // time since response started
	Data   string         `json:"data,omitempty"`
}

// Interaction captures a single HTTP request/response exchange (used in memory).
type Interaction struct {
	Request  RecordedRequest  `json:"request"`
	Response RecordedResponse `json:"response"`
}

// RecordedRequest captures the details of an HTTP request.
type RecordedRequest struct {
	Method string          `json:"method"`
	URL    string          `json:"url"`
	Header http.Header     `json:"header,omitempty"`
	Body   json.RawMessage `json:"body,omitempty"`
}

// RecordedResponse captures the details of an HTTP response.
type RecordedResponse struct {
	Status int             `json:"status"`
	Header http.Header     `json:"header,omitempty"`
	Body   json.RawMessage `json:"body,omitempty"`   // non-streaming responses
	Events []SSEEvent      `json:"events,omitempty"` // streaming (SSE) responses (in-memory only)
}

// SSEEvent captures a single server-sent event with timing information.
type SSEEvent struct {
	Offset time.Duration `json:"offset"` // time since response started
	Data   string        `json:"data"`
}

// Cassette is a collection of HTTP interactions that can be saved to
// and loaded from a JSONL file.
type Cassette struct {
	Interactions []Interaction

	mu   sync.Mutex
	path string
	file *os.File
}

// NewCassette creates a new cassette for recording interactions.
// It creates or truncates the file at the given path.
func NewCassette(path string) (*Cassette, error) {
	file, err := os.Create(path)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create cassette file")
	}

	return &Cassette{
		path: path,
		file: file,
	}, nil
}

// LoadCassette loads a cassette from a JSONL file for replay.
func LoadCassette(path string) (*Cassette, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, errors.Wrap(err, "failed to open cassette file")
	}
	defer file.Close()

	var interactions []Interaction
	scanner := bufio.NewScanner(file)

	// Increase buffer size for large JSON lines
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 10*1024*1024) // 10MB max line size

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var entry Entry
		if err := json.Unmarshal(line, &entry); err != nil {
			// Try legacy format (direct Interaction)
			var interaction Interaction
			if err := json.Unmarshal(line, &interaction); err != nil {
				return nil, errors.Wrap(err, "failed to unmarshal entry")
			}
			interactions = append(interactions, interaction)
			continue
		}

		switch entry.Type {
		case EntryTypeInteraction:
			if entry.Request != nil && entry.Response != nil {
				interactions = append(interactions, Interaction{
					Request:  *entry.Request,
					Response: *entry.Response,
				})
			}
		case EntryTypeStreamStart:
			if entry.Request != nil && entry.Response != nil {
				interactions = append(interactions, Interaction{
					Request:  *entry.Request,
					Response: *entry.Response,
				})
			}
		case EntryTypeSSEEvent:
			// Append to the last interaction's events
			if len(interactions) > 0 && entry.Offset != nil {
				last := &interactions[len(interactions)-1]
				last.Response.Events = append(last.Response.Events, SSEEvent{
					Offset: *entry.Offset,
					Data:   entry.Data,
				})
			}
		default:
			// Try legacy format (direct Interaction without type field)
			if entry.Request != nil && entry.Response != nil {
				interactions = append(interactions, Interaction{
					Request:  *entry.Request,
					Response: *entry.Response,
				})
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, errors.Wrap(err, "failed to read cassette file")
	}

	return &Cassette{
		Interactions: interactions,
		path:         path,
	}, nil
}

// Append writes a single interaction to the cassette file.
// For streaming responses, use AppendStreamStart and AppendSSEEvent instead.
// This is safe for concurrent use.
func (c *Cassette) Append(interaction Interaction) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.file == nil {
		return errors.New("cassette file not open for writing")
	}

	c.Interactions = append(c.Interactions, interaction)

	entry := Entry{
		Type:     EntryTypeInteraction,
		Request:  &interaction.Request,
		Response: &interaction.Response,
	}

	data, err := json.Marshal(entry)
	if err != nil {
		return errors.Wrap(err, "failed to marshal interaction")
	}

	if _, err := c.file.Write(data); err != nil {
		return errors.Wrap(err, "failed to write interaction")
	}

	if _, err := c.file.WriteString("\n"); err != nil {
		return errors.Wrap(err, "failed to write newline")
	}

	return nil
}

// AppendStreamStart writes the start of a streaming interaction (request + response metadata).
// Follow with AppendSSEEvent for each event.
func (c *Cassette) AppendStreamStart(req RecordedRequest, resp RecordedResponse) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.file == nil {
		return errors.New("cassette file not open for writing")
	}

	// Add to in-memory interactions (events will be empty, filled during load)
	c.Interactions = append(c.Interactions, Interaction{
		Request:  req,
		Response: resp,
	})

	entry := Entry{
		Type:     EntryTypeStreamStart,
		Request:  &req,
		Response: &resp,
	}

	data, err := json.Marshal(entry)
	if err != nil {
		return errors.Wrap(err, "failed to marshal stream start")
	}

	if _, err := c.file.Write(data); err != nil {
		return errors.Wrap(err, "failed to write stream start")
	}

	if _, err := c.file.WriteString("\n"); err != nil {
		return errors.Wrap(err, "failed to write newline")
	}

	return nil
}

// AppendSSEEvent writes a single SSE event to the cassette file.
func (c *Cassette) AppendSSEEvent(offset time.Duration, data string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.file == nil {
		return errors.New("cassette file not open for writing")
	}

	// Add to the last interaction's events in memory
	if len(c.Interactions) > 0 {
		last := &c.Interactions[len(c.Interactions)-1]
		last.Response.Events = append(last.Response.Events, SSEEvent{
			Offset: offset,
			Data:   data,
		})
	}

	entry := Entry{
		Type:   EntryTypeSSEEvent,
		Offset: &offset,
		Data:   data,
	}

	entryData, err := json.Marshal(entry)
	if err != nil {
		return errors.Wrap(err, "failed to marshal SSE event")
	}

	if _, err := c.file.Write(entryData); err != nil {
		return errors.Wrap(err, "failed to write SSE event")
	}

	if _, err := c.file.WriteString("\n"); err != nil {
		return errors.Wrap(err, "failed to write newline")
	}

	return nil
}

// Close closes the cassette file.
func (c *Cassette) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.file == nil {
		return nil
	}

	err := c.file.Close()
	c.file = nil
	return errors.Wrap(err, "error closing cassette file")
}

// Match finds a recorded interaction that matches the given request.
// It matches by method and URL. Returns nil if no match is found.
func (c *Cassette) Match(req *http.Request) *Interaction {
	for i := range c.Interactions {
		interaction := &c.Interactions[i]
		if interaction.Request.Method == req.Method &&
			interaction.Request.URL == req.URL.String() {
			return interaction
		}
	}
	return nil
}

// MatchAndConsume finds and removes a matching interaction from the cassette.
// This is useful when the same endpoint may be called multiple times
// with different responses.
func (c *Cassette) MatchAndConsume(req *http.Request) *Interaction {
	c.mu.Lock()
	defer c.mu.Unlock()

	for i := range c.Interactions {
		interaction := &c.Interactions[i]
		if interaction.Request.Method == req.Method &&
			interaction.Request.URL == req.URL.String() {
			// Remove this interaction from the slice
			result := *interaction
			c.Interactions = append(c.Interactions[:i], c.Interactions[i+1:]...)
			return &result
		}
	}
	return nil
}
