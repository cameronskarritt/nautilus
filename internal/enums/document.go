package enums

type DocumentStatus string

const (
	DocumentStatusUploading DocumentStatus = "uploading"
	DocumentStatusUploaded  DocumentStatus = "uploaded"
	DocumentStatusFailed    DocumentStatus = "failed"
)

func (s DocumentStatus) String() string {
	return string(s)
}
