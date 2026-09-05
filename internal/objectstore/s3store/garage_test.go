package s3store_test

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"

	"nautilus/internal/objectstore"
	"nautilus/internal/objectstore/s3store"
	"nautilus/internal/optional"
	"nautilus/internal/testutil/require"
)

// Run against the local Compose service with GARAGE_TEST_ENDPOINT=http://localhost:3900.
func TestStore_Garage(t *testing.T) {
	endpoint := os.Getenv("GARAGE_TEST_ENDPOINT")
	if endpoint == "" {
		t.Skip("GARAGE_TEST_ENDPOINT is not set")
	}
	t.Parallel()
	store := s3store.New(aws.Config{
		Region:       "us-east-1",
		BaseEndpoint: aws.String(endpoint),
		Credentials: credentials.NewStaticCredentialsProvider(
			"GK00000000000000000000000000000000", strings.Repeat("0", 64), "",
		),
	}, "nautilus-dev", true)
	ctx := t.Context()
	prefix := fmt.Sprintf("test/%d/", time.Now().UnixNano())
	key, copyKey := prefix+"source +%.txt", prefix+"copy.txt"
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		require.NoError(t, store.Delete(cleanupCtx, key))
		require.NoError(t, store.Delete(cleanupCtx, copyKey))
	})
	require.NoError(t, store.Put(ctx, key, strings.NewReader("hello world"), &objectstore.PutOptions{
		ContentType: optional.Set("text/plain"),
		Metadata:    optional.Set(map[string]string{"author": "test"}),
	}))
	info, err := store.Head(ctx, key)
	require.NoError(t, err)
	require.Equal(t, int64(11), info.Size)
	require.Equal(t, "text/plain", info.ContentType)
	require.Equal(t, "test", info.Metadata["author"])
	require.False(t, info.LastModified.IsZero())

	obj, err := store.Get(ctx, key, &objectstore.GetOptions{
		Offset: optional.Set[int64](6),
		Length: optional.Set[int64](5),
	})
	require.NoError(t, err)
	body, err := io.ReadAll(obj.Body)
	require.NoError(t, obj.Body.Close())
	require.NoError(t, err)
	require.Equal(t, "world", string(body))

	require.NoError(t, store.Copy(ctx, key, copyKey))
	obj, err = store.Get(ctx, copyKey, nil)
	require.NoError(t, err)
	body, err = io.ReadAll(obj.Body)
	require.NoError(t, obj.Body.Close())
	require.NoError(t, err)
	require.Equal(t, "hello world", string(body))
	require.Equal(t, "test", obj.Metadata["author"])

	objects, err := store.List(ctx, prefix)
	require.NoError(t, err)
	require.Len(t, objects, 2)
	require.Equal(t, copyKey, objects[0].Key)
	require.Equal(t, key, objects[1].Key)
	require.NoError(t, store.Delete(ctx, key))
	require.NoError(t, store.Delete(ctx, key))
	_, err = store.Get(ctx, key, nil)
	require.ErrorIs(t, err, objectstore.ErrNotFound)
	_, err = store.Head(ctx, key)
	require.ErrorIs(t, err, objectstore.ErrNotFound)
	require.ErrorIs(t, store.Copy(ctx, key, prefix+"missing.txt"), objectstore.ErrNotFound)
}
