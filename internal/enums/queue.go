package enums

type Queue string

const (
	QueueSmoke   Queue = "smoke"
	QueueUploads Queue = "uploads"
)

func (q Queue) String() string {
	return string(q)
}
