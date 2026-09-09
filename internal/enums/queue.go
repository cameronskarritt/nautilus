package enums

type Queue string

const (
	QueueSmoke    Queue = "smoke"
	QueueUploads  Queue = "uploads"
	QueueWebhooks Queue = "webhooks"
)

func (q Queue) String() string {
	return string(q)
}
