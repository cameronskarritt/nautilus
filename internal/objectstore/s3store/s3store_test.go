package s3store

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"

	"nautilus/internal/errors"
	"nautilus/internal/objectstore"
	"nautilus/internal/optional"
	"nautilus/internal/testutil/require"
)

func testStore(t *testing.T, handler http.HandlerFunc) *Store {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return New(aws.Config{
		Region:       "us-east-1",
		BaseEndpoint: aws.String(server.URL),
		Credentials:  aws.AnonymousCredentials{},
	}, "test-bucket", true)
}

func TestStore_Put(t *testing.T) {
	t.Parallel()
	type request struct {
		method, path, contentType, author, body string
		err                                     error
	}
	requests := make(chan request, 1)
	store := testStore(t, func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		requests <- request{r.Method, r.URL.Path, r.Header.Get("Content-Type"), r.Header.Get("X-Amz-Meta-Author"), string(body), err}
		w.WriteHeader(http.StatusOK)
	})
	err := store.Put(t.Context(), "docs/file.txt", strings.NewReader("hello"), &objectstore.PutOptions{
		ContentType: optional.Set("text/plain"),
		Metadata:    optional.Set(map[string]string{"author": "test"}),
	})
	require.NoError(t, err)
	got := <-requests
	require.NoError(t, got.err)
	require.Equal(t, http.MethodPut, got.method)
	require.Equal(t, "/test-bucket/docs/file.txt", got.path)
	require.Equal(t, "text/plain", got.contentType)
	require.Equal(t, "test", got.author)
	require.Equal(t, "hello", got.body)
}

func TestStore_Get(t *testing.T) {
	t.Parallel()
	requests := make(chan string, 1)
	store := testStore(t, func(w http.ResponseWriter, r *http.Request) {
		requests <- r.Header.Get("Range")
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("X-Amz-Meta-Author", "test")
		w.Header().Set("Last-Modified", "Sat, 05 Sep 2026 12:00:00 GMT")
		w.Header().Set("Content-Range", "bytes 6-10/11")
		w.WriteHeader(http.StatusPartialContent)
		_, _ = io.WriteString(w, "world")
	})
	obj, err := store.Get(t.Context(), "docs/file.txt", &objectstore.GetOptions{
		Offset: optional.Set[int64](6),
		Length: optional.Set[int64](5),
	})
	require.NoError(t, err)
	t.Cleanup(func() { obj.Body.Close() })
	body, err := io.ReadAll(obj.Body)
	require.NoError(t, err)
	require.Equal(t, "world", string(body))
	require.Equal(t, "bytes=6-10", <-requests)
	require.Equal(t, "docs/file.txt", obj.Key)
	require.Equal(t, int64(5), obj.Size)
	require.Equal(t, "text/plain", obj.ContentType)
	require.Equal(t, "test", obj.Metadata["author"])
	require.False(t, obj.LastModified.IsZero())
}

func TestStore_NotFound(t *testing.T) {
	t.Parallel()
	for _, method := range []string{"get", "head", "copy"} {
		t.Run(method, func(t *testing.T) {
			t.Parallel()
			store := testStore(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/xml")
				w.WriteHeader(http.StatusNotFound)
				if r.Method != http.MethodHead {
					_, _ = io.WriteString(w, "<Error><Code>NoSuchKey</Code><Message>Missing</Message></Error>")
				}
			})
			var err error
			switch method {
			case "get":
				_, err = store.Get(t.Context(), "missing", nil)
			case "head":
				_, err = store.Head(t.Context(), "missing")
			case "copy":
				err = store.Copy(t.Context(), "missing", "copy")
			}
			require.ErrorIs(t, err, objectstore.ErrNotFound)
		})
	}
}

func TestByteRange(t *testing.T) {
	t.Parallel()

	tests := []struct {
		Name string
		Opts *objectstore.GetOptions
		Want string
	}{
		{
			Name: "offset and length",
			Opts: &objectstore.GetOptions{
				Offset: optional.Set[int64](10),
				Length: optional.Set[int64](5),
			},
			Want: "bytes=10-14",
		},
		{
			Name: "offset only",
			Opts: &objectstore.GetOptions{
				Offset: optional.Set[int64](10),
			},
			Want: "bytes=10-",
		},
		{
			Name: "length only",
			Opts: &objectstore.GetOptions{
				Length: optional.Set[int64](5),
			},
			Want: "bytes=0-4",
		},
		{
			Name: "zero offset and length",
			Opts: &objectstore.GetOptions{
				Offset: optional.Set[int64](0),
				Length: optional.Set[int64](0),
			},
		},
		{
			Name: "negative values",
			Opts: &objectstore.GetOptions{
				Offset: optional.Set[int64](-1),
				Length: optional.Set[int64](-1),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.Want, byteRange(tt.Opts))
		})
	}
}

func TestIsNotFound(t *testing.T) {
	t.Parallel()

	tests := []struct {
		Name string
		Err  error
		Want bool
	}{
		{
			Name: "no such key",
			Err:  &types.NoSuchKey{},
			Want: true,
		},
		{
			Name: "not found",
			Err:  &types.NotFound{},
			Want: true,
		},
		{
			Name: "wrapped no such key",
			Err:  errors.Wrap(&types.NoSuchKey{}, "get object"),
			Want: true,
		},
		{
			Name: "other error",
			Err:  errors.New("boom"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.Want, isNotFound(tt.Err))
		})
	}
}
