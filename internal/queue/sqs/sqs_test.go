package sqs

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"

	"nautilus/internal/enums"
	"nautilus/internal/errors"
	"nautilus/internal/log"
	"nautilus/internal/testutil/require"
)

type testClient struct {
	messages     chan types.Message
	sent         chan *sqs.SendMessageInput
	deleted      chan *sqs.DeleteMessageInput
	visibility   chan *sqs.ChangeMessageVisibilityInput
	queueURL     string
	sqsMessageID string
}

func newTestClient() *testClient {
	return &testClient{
		messages:     make(chan types.Message, 1),
		sent:         make(chan *sqs.SendMessageInput, 1),
		deleted:      make(chan *sqs.DeleteMessageInput, 1),
		visibility:   make(chan *sqs.ChangeMessageVisibilityInput, 1),
		queueURL:     "https://sqs.example/test",
		sqsMessageID: "sqs-message-1",
	}
}

func (c *testClient) GetQueueUrl(
	context.Context,
	*sqs.GetQueueUrlInput,
	...func(*sqs.Options),
) (*sqs.GetQueueUrlOutput, error) {
	return &sqs.GetQueueUrlOutput{QueueUrl: &c.queueURL}, nil
}

func (c *testClient) SendMessage(
	_ context.Context,
	input *sqs.SendMessageInput,
	_ ...func(*sqs.Options),
) (*sqs.SendMessageOutput, error) {
	c.sent <- input
	return &sqs.SendMessageOutput{MessageId: &c.sqsMessageID}, nil
}

func (c *testClient) ReceiveMessage(
	ctx context.Context,
	_ *sqs.ReceiveMessageInput,
	_ ...func(*sqs.Options),
) (*sqs.ReceiveMessageOutput, error) {
	select {
	case received := <-c.messages:
		return &sqs.ReceiveMessageOutput{Messages: []types.Message{received}}, nil
	case <-ctx.Done():
		return nil, errors.Wrap(ctx.Err(), "receive interrupted")
	}
}

func (c *testClient) DeleteMessage(
	_ context.Context,
	input *sqs.DeleteMessageInput,
	_ ...func(*sqs.Options),
) (*sqs.DeleteMessageOutput, error) {
	c.deleted <- input
	return new(sqs.DeleteMessageOutput), nil
}

func (c *testClient) ChangeMessageVisibility(
	_ context.Context,
	input *sqs.ChangeMessageVisibilityInput,
	_ ...func(*sqs.Options),
) (*sqs.ChangeMessageVisibilityOutput, error) {
	c.visibility <- input
	return new(sqs.ChangeMessageVisibilityOutput), nil
}

func newTestBroker(client *testClient) *Broker {
	return &Broker{
		client:            client,
		logger:            log.Default(),
		visibilityTimeout: 300,
		queueURLs:         make(map[enums.Queue]string),
	}
}

func TestBrokerPublishOwnsEnvelope(t *testing.T) {
	t.Parallel()

	client := newTestClient()
	broker := newTestBroker(client)

	messageID, err := broker.Publish(t.Context(), "test", map[string]string{"key": "value"})
	require.NoError(t, err)
	require.NotEqual(t, "", messageID)

	sent := waitSQS(t, client.sent)
	require.Equal(t, client.queueURL, *sent.QueueUrl)

	var envelope message
	require.NoError(t, json.Unmarshal([]byte(*sent.MessageBody), &envelope))
	require.Equal(t, messageID, envelope.ID)
	require.JSONEq(t, `{"key":"value"}`, string(envelope.Data))
}

func TestBrokerConsumeOwnsDeliveryLifecycle(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		body           string
		handlerErr     error
		wantHandled    bool
		wantDelete     bool
		wantVisibility bool
	}{
		{
			name:        "acks successful delivery",
			body:        `{"id":"message-1","data":{"ok":true}}`,
			wantHandled: true,
			wantDelete:  true,
		},
		{
			name:           "requeues failed delivery",
			body:           `{"id":"message-1","data":{"ok":true}}`,
			handlerErr:     errors.New("handler failed"),
			wantHandled:    true,
			wantVisibility: true,
		},
		{
			name:       "deletes poison envelope",
			body:       `{`,
			wantDelete: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			client := newTestClient()
			receiptHandle := "receipt-1"
			client.messages <- types.Message{
				Body:          &tt.body,
				ReceiptHandle: &receiptHandle,
			}
			broker := newTestBroker(client)
			ctx, cancel := context.WithCancel(t.Context())
			handled := make(chan []byte, 1)
			done := make(chan error, 1)
			go func() {
				done <- broker.Consume(ctx, "test", func(_ context.Context, data []byte) error {
					handled <- data
					return tt.handlerErr
				})
			}()

			if tt.wantHandled {
				require.JSONEq(t, `{"ok":true}`, string(waitSQS(t, handled)))
			}
			if tt.wantDelete {
				deleted := waitSQS(t, client.deleted)
				require.Equal(t, receiptHandle, *deleted.ReceiptHandle)
			}
			if tt.wantVisibility {
				visibility := waitSQS(t, client.visibility)
				require.Equal(t, receiptHandle, *visibility.ReceiptHandle)
				require.Equal(t, nackRequeueDelay, visibility.VisibilityTimeout)
			}

			cancel()
			require.NoError(t, waitSQS(t, done))
			if !tt.wantHandled {
				require.Len(t, handled, 0)
			}
		})
	}
}

func waitSQS[T any](t *testing.T, ch <-chan T) T {
	t.Helper()

	select {
	case value := <-ch:
		return value
	case <-time.After(time.Second):
		var zero T
		t.Fatal("timed out waiting for value")
		return zero
	}
}
