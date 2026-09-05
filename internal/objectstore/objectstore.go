package objectstore

import (
	"context"
	"io"
	"time"

	"nautilus/internal/errors"
	"nautilus/internal/optional"
)

var (
	ErrNotFound   = errors.New("object not found")
	ErrInvalidKey = errors.New("invalid key")
)

type Store interface {
	Put(ctx context.Context, key string, data io.Reader, opts *PutOptions) error
	Get(ctx context.Context, key string, opts *GetOptions) (*Object, error)
	Delete(ctx context.Context, key string) error
	Head(ctx context.Context, key string) (*ObjectInfo, error)
	List(ctx context.Context, prefix string) ([]ObjectInfo, error)
	Copy(ctx context.Context, srcKey, dstKey string) error
}

type Object struct {
	ObjectInfo
	Body io.ReadCloser
}

type ObjectInfo struct {
	Key          string
	Size         int64
	LastModified time.Time
	ContentType  string
	Metadata     map[string]string
}

type PutOptions struct {
	ContentType optional.Optional[string]
	Metadata    optional.Optional[map[string]string]
}

type GetOptions struct {
	Offset optional.Optional[int64] // byte offset to start reading from
	Length optional.Optional[int64] // number of bytes to read
}
