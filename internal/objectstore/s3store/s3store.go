package s3store

import (
	"context"
	"io"
	"net/url"
	"strconv"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"

	"nautilus/internal/errors"
	"nautilus/internal/objectstore"
	"nautilus/internal/util"
)

var _ objectstore.Store = new(Store)

type Store struct {
	client *s3.Client
	bucket string
}

func New(cfg aws.Config, bucket string, usePathStyle bool) *Store {
	client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.UsePathStyle = usePathStyle
	})
	return &Store{
		client: client,
		bucket: bucket,
	}
}

func (s *Store) Put(ctx context.Context, key string, data io.Reader, opts *objectstore.PutOptions) error {
	if key == "" {
		return objectstore.ErrInvalidKey
	}

	input := &s3.PutObjectInput{
		Bucket: &s.bucket,
		Key:    &key,
		Body:   data,
	}

	if opts != nil {
		if opts.ContentType.Set {
			input.ContentType = &opts.ContentType.Data
		}
		if opts.Metadata.Set {
			input.Metadata = opts.Metadata.Data
		}
	}

	_, err := s.client.PutObject(ctx, input)
	if err != nil {
		return errors.Wrapf(err, "s3: failed to put object: %s", key)
	}

	return nil
}

func (s *Store) Get(ctx context.Context, key string, opts *objectstore.GetOptions) (*objectstore.Object, error) {
	if key == "" {
		return nil, objectstore.ErrInvalidKey
	}

	input := &s3.GetObjectInput{
		Bucket: &s.bucket,
		Key:    &key,
	}

	if opts != nil {
		if header := byteRange(opts); header != "" {
			input.Range = &header
		}
	}

	out, err := s.client.GetObject(ctx, input)
	if err != nil {
		if isNotFound(err) {
			return nil, objectstore.ErrNotFound
		}
		return nil, errors.Wrapf(err, "s3: failed to get object: %s", key)
	}

	obj := &objectstore.Object{
		ObjectInfo: objectstore.ObjectInfo{
			Key:      key,
			Size:     util.Deref(out.ContentLength),
			Metadata: out.Metadata,
		},
		Body: out.Body,
	}

	obj.ContentType = util.Deref(out.ContentType)
	obj.LastModified = util.Deref(out.LastModified)

	return obj, nil
}

func (s *Store) Delete(ctx context.Context, key string) error {
	if key == "" {
		return objectstore.ErrInvalidKey
	}

	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: &s.bucket,
		Key:    &key,
	})
	if err != nil {
		return errors.Wrapf(err, "s3: failed to delete object: %s", key)
	}

	return nil
}

func (s *Store) Head(ctx context.Context, key string) (*objectstore.ObjectInfo, error) {
	if key == "" {
		return nil, objectstore.ErrInvalidKey
	}

	out, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: &s.bucket,
		Key:    &key,
	})
	if err != nil {
		if isNotFound(err) {
			return nil, objectstore.ErrNotFound
		}
		return nil, errors.Wrapf(err, "s3: failed to head object: %s", key)
	}

	info := &objectstore.ObjectInfo{
		Key:      key,
		Size:     util.Deref(out.ContentLength),
		Metadata: out.Metadata,
	}

	info.ContentType = util.Deref(out.ContentType)
	info.LastModified = util.Deref(out.LastModified)

	return info, nil
}

func (s *Store) List(ctx context.Context, prefix string) ([]objectstore.ObjectInfo, error) {
	input := &s3.ListObjectsV2Input{
		Bucket: &s.bucket,
	}
	if prefix != "" {
		input.Prefix = &prefix
	}

	var results []objectstore.ObjectInfo

	paginator := s3.NewListObjectsV2Paginator(s.client, input)
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, errors.Wrapf(err, "s3: failed to list objects with prefix: %s", prefix)
		}
		for _, obj := range page.Contents {
			results = append(results, objectstore.ObjectInfo{
				Key:          util.Deref(obj.Key),
				Size:         util.Deref(obj.Size),
				LastModified: util.Deref(obj.LastModified),
			})
		}
	}

	if results == nil {
		results = []objectstore.ObjectInfo{}
	}

	return results, nil
}

func (s *Store) Copy(ctx context.Context, srcKey, dstKey string) error {
	if srcKey == "" || dstKey == "" {
		return objectstore.ErrInvalidKey
	}

	copySource := s.bucket + "/" + url.PathEscape(srcKey)

	_, err := s.client.CopyObject(ctx, &s3.CopyObjectInput{
		Bucket:     &s.bucket,
		Key:        &dstKey,
		CopySource: &copySource,
	})
	if err != nil {
		if isNotFound(err) {
			return objectstore.ErrNotFound
		}
		return errors.Wrapf(err, "s3: failed to copy object: %s -> %s", srcKey, dstKey)
	}

	return nil
}

func byteRange(opts *objectstore.GetOptions) string {
	if opts.Offset.Set && opts.Offset.Data > 0 {
		if opts.Length.Set && opts.Length.Data > 0 {
			end := opts.Offset.Data + opts.Length.Data - 1
			return "bytes=" + strconv.FormatInt(opts.Offset.Data, 10) + "-" + strconv.FormatInt(end, 10)
		}
		return "bytes=" + strconv.FormatInt(opts.Offset.Data, 10) + "-"
	}
	if opts.Length.Set && opts.Length.Data > 0 {
		end := opts.Length.Data - 1
		return "bytes=0-" + strconv.FormatInt(end, 10)
	}
	return ""
}

func isNotFound(err error) bool {
	var apiErr smithy.APIError
	return errors.As(err, &apiErr) && (apiErr.ErrorCode() == "NoSuchKey" || apiErr.ErrorCode() == "NotFound")
}
