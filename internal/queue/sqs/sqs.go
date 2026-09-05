package sqs

import (
	"context"
	"encoding/json"
	"math/rand/v2"
	"sync"
	"time"
	"uuid"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"

	"nautilus/internal/enums"
	"nautilus/internal/errors"
	"nautilus/internal/log"
	"nautilus/internal/queue"
	"nautilus/internal/util"
)

const (
	nackRequeueDelay = int32(5)
	pollMinBackoff   = 2 * time.Second
	pollMaxBackoff   = 60 * time.Second
)

var _ queue.Publisher = new(Broker)
var _ queue.Consumer = new(Broker)

type sqsClient interface {
	GetQueueUrl(context.Context, *sqs.GetQueueUrlInput, ...func(*sqs.Options)) (*sqs.GetQueueUrlOutput, error)
	SendMessage(context.Context, *sqs.SendMessageInput, ...func(*sqs.Options)) (*sqs.SendMessageOutput, error)
	ReceiveMessage(context.Context, *sqs.ReceiveMessageInput, ...func(*sqs.Options)) (*sqs.ReceiveMessageOutput, error)
	DeleteMessage(context.Context, *sqs.DeleteMessageInput, ...func(*sqs.Options)) (*sqs.DeleteMessageOutput, error)
	ChangeMessageVisibility(context.Context, *sqs.ChangeMessageVisibilityInput, ...func(*sqs.Options)) (*sqs.ChangeMessageVisibilityOutput, error)
}

type Broker struct {
	client sqsClient
	logger *log.Logger

	// queuePrefix is prepended to enum queue names to form SQS queue names.
	queuePrefix string

	// visibilityTimeout is the SQS visibility timeout in seconds. It controls
	// how long a message is hidden from other consumers after being received.
	visibilityTimeout int32

	// queueURLs caches resolved SQS queue URLs.
	queueURLs map[enums.Queue]string

	mu sync.Mutex
}

type message struct {
	ID   string          `json:"id"`
	Data json.RawMessage `json:"data"`
}

type delivery struct {
	id            string
	data          json.RawMessage
	queueURL      string
	receiptHandle string
	cancelBeat    context.CancelFunc
	beatDone      <-chan struct{}
}

func NewBroker(cfg aws.Config, logger *log.Logger, queuePrefix string, visibilityTimeout int32) *Broker {
	return &Broker{
		client:            sqs.NewFromConfig(cfg),
		logger:            logger,
		queuePrefix:       queuePrefix,
		visibilityTimeout: visibilityTimeout,
		queueURLs:         make(map[enums.Queue]string),
	}
}

func (b *Broker) Publish(ctx context.Context, topic enums.Queue, data any) (string, error) {
	queueURL, err := b.resolveQueueURL(ctx, topic)
	if err != nil {
		return "", errors.Wrapf(err, "sqs: unable to resolve queue URL: %s", topic)
	}

	buf, err := json.Marshal(data)
	if err != nil {
		return "", errors.Wrapf(err, "sqs: unable to marshal data")
	}

	msg := message{
		ID:   uuid.New().String(),
		Data: buf,
	}

	body, err := json.Marshal(msg)
	if err != nil {
		return "", errors.Wrapf(err, "sqs: unable to marshal message envelope")
	}

	bodyStr := string(body)
	out, err := b.client.SendMessage(ctx, &sqs.SendMessageInput{
		QueueUrl:    &queueURL,
		MessageBody: &bodyStr,
	})
	if err != nil {
		return "", errors.Wrapf(err, "sqs: failed to send message to %s", topic)
	}

	b.logger.Info("published message to SQS",
		"queue", string(topic),
		"message_id", msg.ID,
		"sqs_message_id", util.Deref(out.MessageId),
	)

	return msg.ID, nil
}

func (b *Broker) Consume(ctx context.Context, q enums.Queue, handler queue.MessageHandler) error {
	queueURL, err := b.resolveQueueURL(ctx, q)
	if err != nil {
		return err
	}

	deliveries := make(chan delivery, 100)
	pollCtx, cancel := context.WithCancel(ctx)
	var wg sync.WaitGroup
	wg.Add(1)
	go b.poll(pollCtx, q, queueURL, deliveries, &wg)
	defer func() {
		cancel()
		wg.Wait()
	}()

	for {
		select {
		case <-ctx.Done():
			return nil
		case delivery, ok := <-deliveries:
			if !ok {
				return nil
			}

			err := handler(ctx, delivery.data)
			delivery.cancelHeartbeat()
			if err != nil {
				b.logger.Error("error consuming from queue", "message_id", delivery.id, "error", err)
				if nackErr := b.nack(ctx, delivery); nackErr != nil {
					b.logger.Error("unable to nack message", "message_id", delivery.id, "error", nackErr)
				}
				continue
			}

			if ackErr := b.ack(ctx, delivery); ackErr != nil {
				b.logger.Error("unable to ack message", "message_id", delivery.id, "error", ackErr)
			}
		}
	}
}

func (d delivery) cancelHeartbeat() {
	if d.cancelBeat != nil {
		d.cancelBeat()
		<-d.beatDone
	}
}

func (b *Broker) ack(ctx context.Context, delivery delivery) error {
	if delivery.receiptHandle == "" {
		b.logger.Warn("sqs: unable to ack message without receipt handle", "message_id", delivery.id)
		return nil
	}
	_, err := b.client.DeleteMessage(ctx, &sqs.DeleteMessageInput{
		QueueUrl:      &delivery.queueURL,
		ReceiptHandle: &delivery.receiptHandle,
	})
	if err != nil {
		return errors.Wrapf(err, "sqs: failed to delete message %s", delivery.id)
	}

	return nil
}

func (b *Broker) nack(ctx context.Context, delivery delivery) error {
	if delivery.receiptHandle == "" {
		b.logger.Warn("sqs: unable to nack message without receipt handle", "message_id", delivery.id)
		return nil
	}

	_, err := b.client.ChangeMessageVisibility(ctx, &sqs.ChangeMessageVisibilityInput{
		QueueUrl:          &delivery.queueURL,
		ReceiptHandle:     &delivery.receiptHandle,
		VisibilityTimeout: nackRequeueDelay,
	})
	if err != nil {
		return errors.Wrapf(err, "sqs: failed to change visibility for message %s", delivery.id)
	}

	return nil
}

// poll long-polls an SQS queue and feeds received messages into the delivery channel.
func (b *Broker) poll(
	ctx context.Context,
	q enums.Queue,
	queueURL string,
	deliveries chan<- delivery,
	wg *sync.WaitGroup,
) {
	defer wg.Done()
	defer close(deliveries)

	backoff := pollMinBackoff

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		out, err := b.client.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{
			QueueUrl:            &queueURL,
			MaxNumberOfMessages: 10,
			WaitTimeSeconds:     20,
			VisibilityTimeout:   b.visibilityTimeout,
		})
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			b.logger.Error("sqs: error receiving messages", "queue", string(q), "error", err)
			jitter := time.Duration(rand.Int64N(int64(backoff) / 2))
			timer := time.NewTimer(backoff + jitter)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
			backoff = min(backoff*2, pollMaxBackoff)
			continue
		}

		backoff = pollMinBackoff

		for _, sqsMsg := range out.Messages {
			if sqsMsg.Body == nil {
				continue
			}

			var envelope message
			if err := json.Unmarshal([]byte(*sqsMsg.Body), &envelope); err != nil {
				b.logger.Error("sqs: failed to unmarshal message envelope, deleting poison message",
					"queue", string(q), "error", err)
				if sqsMsg.ReceiptHandle != nil {
					_, _ = b.client.DeleteMessage(ctx, &sqs.DeleteMessageInput{
						QueueUrl:      &queueURL,
						ReceiptHandle: sqsMsg.ReceiptHandle,
					})
				}
				continue
			}

			delivery := delivery{
				id:       envelope.ID,
				data:     envelope.Data,
				queueURL: queueURL,
			}
			if sqsMsg.ReceiptHandle != nil {
				beatCtx, cancelBeat := context.WithCancel(ctx)
				beatDone := make(chan struct{})
				delivery.receiptHandle = *sqsMsg.ReceiptHandle
				delivery.cancelBeat = cancelBeat
				delivery.beatDone = beatDone
				wg.Add(1)
				go b.heartbeat(beatCtx, envelope.ID, queueURL, *sqsMsg.ReceiptHandle, beatDone, wg)
			}

			select {
			case deliveries <- delivery:
			case <-ctx.Done():
				delivery.cancelHeartbeat()
				return
			}
		}
	}
}

// heartbeat periodically extends the SQS visibility timeout for a message
// that is still being processed. Its context is cancelled when the delivery
// finishes or the consumer shuts down.
func (b *Broker) heartbeat(
	ctx context.Context,
	messageID string,
	queueURL string,
	receiptHandle string,
	done chan<- struct{},
	wg *sync.WaitGroup,
) {
	defer wg.Done()
	defer close(done)

	interval := time.Duration(b.visibilityTimeout) * time.Second / 2
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_, err := b.client.ChangeMessageVisibility(ctx, &sqs.ChangeMessageVisibilityInput{
				QueueUrl:          &queueURL,
				ReceiptHandle:     &receiptHandle,
				VisibilityTimeout: b.visibilityTimeout,
			})
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				b.logger.Warn("sqs: heartbeat failed to extend visibility",
					"message_id", messageID, "error", err)
				return
			}
		}
	}
}

func (b *Broker) resolveQueueURL(ctx context.Context, q enums.Queue) (string, error) {
	b.mu.Lock()
	if url, ok := b.queueURLs[q]; ok {
		b.mu.Unlock()
		return url, nil
	}
	b.mu.Unlock()

	name := b.queuePrefix + string(q)
	out, err := b.client.GetQueueUrl(ctx, &sqs.GetQueueUrlInput{
		QueueName: &name,
	})
	if err != nil {
		return "", errors.Wrapf(err, "sqs: failed to get queue URL for %s", name)
	}

	url := util.Deref(out.QueueUrl)

	b.mu.Lock()
	b.queueURLs[q] = url
	b.mu.Unlock()

	return url, nil
}
