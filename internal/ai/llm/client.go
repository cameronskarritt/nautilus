package llm

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"time"

	"nautilus/internal/enums"
	"nautilus/internal/errors"
	"nautilus/internal/optional"
)

type ServerSentEvent struct {
	Event []byte `json:"event"`
	Data  []byte `json:"data"`
}

func (e *ServerSentEvent) IsEmpty() bool {
	return len(e.Event) == 0 && len(e.Data) == 0
}

type EventStreamOptions struct {
	CompletionTimeout optional.Optional[time.Duration]
	TokenDelayTimeout optional.Optional[time.Duration]
	TokenStream       optional.Optional[TokenStreamFunc]
}

type EventStream struct {
	tokenDelayTimeout time.Duration
	tokenStream       TokenStreamFunc
}

func NewEventStream(opts *EventStreamOptions) *EventStream {
	if opts == nil {
		opts = new(EventStreamOptions)
	}

	return &EventStream{
		tokenDelayTimeout: opts.TokenDelayTimeout.Or(2 * time.Second),
		tokenStream:       opts.TokenStream.Or(NewTokenStream),
	}
}

type EventHandler func(sse *ServerSentEvent) (Token, error)

func (c *EventStream) HandleEvents(ctx context.Context, resp *http.Response, handler EventHandler) TokenStream {
	// logger := log.FromContext(ctx)
	tokens := make(chan Token, 1)

	go func(body io.ReadCloser) {
		defer func() {
			if r := recover(); r != nil {
				// logger.Error("handled panic", "error", r)
				SendToken(ctx, tokens, &ErrorToken{
					Type: enums.TokenTypeError,
					Err:  errors.Errorf("panic: %v", r),
				})
			}
			body.Close()
			close(tokens)
		}()

		sse := bufio.NewReader(body)

		tokenDelay := time.NewTimer(c.tokenDelayTimeout)
		defer tokenDelay.Stop()

		var event ServerSentEvent
		for {
			select {
			case <-ctx.Done():
				err := ctx.Err()
				if errors.Is(err, context.Canceled) {
					SendToken(ctx, tokens, &ErrorToken{
						Type: enums.TokenTypeError,
						Err:  ErrStreamCanceled,
					})
				}

				if errors.Is(err, context.DeadlineExceeded) {
					SendToken(ctx, tokens, &ErrorToken{
						Type: enums.TokenTypeError,
						Err:  ErrCompletionTimeout,
					})
				}
				return
			case <-tokenDelay.C:
				SendToken(ctx, tokens, &ErrorToken{
					Type: enums.TokenTypeError,
					Err:  ErrTokenDelayTimeout,
				})
				return
			default:
				line, err := sse.ReadBytes('\n')
				if err != nil {
					if errors.Is(err, io.EOF) {
						return
					}
					var netErr net.Error
					if errors.As(err, &netErr) && netErr.Timeout() {
						SendToken(ctx, tokens, &ErrorToken{
							Type: enums.TokenTypeError,
							Err:  ErrCompletionTimeout,
						})
						return
					}
					SendToken(ctx, tokens, &ErrorToken{
						Type: enums.TokenTypeError,
						Err:  errors.Wrap(err, "error reading server sent event"),
					})
					return
				}
				tokenDelay.Reset(c.tokenDelayTimeout)

				line = bytes.TrimSpace(line)

				// Per the spec, lines starting with a colon are comments and should be ignored
				if bytes.HasPrefix(line, []byte(":")) {
					continue
				}

				// Blank line indicates the end of the event, dispatch the event if it's not empty
				if len(line) == 0 && !event.IsEmpty() {
					token, err := handler(&event)
					if err != nil {
						SendToken(ctx, tokens, &ErrorToken{
							Type: enums.TokenTypeError,
							Err:  err,
						})
						return
					}
					if token != nil {
						SendToken(ctx, tokens, token)
					}

					// Reset the event
					event = ServerSentEvent{}
					continue
				}

				if bytes.HasPrefix(line, []byte("event:")) {
					event.Event = bytes.TrimSpace(bytes.TrimPrefix(line, []byte("event:")))
					continue
				}

				if bytes.HasPrefix(line, []byte("data:")) {
					data := bytes.TrimSpace(bytes.TrimPrefix(line, []byte("data:")))
					if len(event.Data) == 0 {
						event.Data = data
					} else {
						event.Data = append(event.Data, '\n')
						event.Data = append(event.Data, data...)
					}
				}
			}
		}
	}(resp.Body)

	var cancel context.CancelFunc
	ctx, cancel = context.WithCancel(ctx)

	return WithMetrics(c.tokenStream(tokens, cancel))
}
