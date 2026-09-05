package inmemory

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"nautilus/internal/objectstore"
	"nautilus/internal/optional"
	"nautilus/internal/testutil/require"
)

func setupTestStore(t *testing.T) (*MemoryObjectStore, string) {
	t.Helper()
	dir := t.TempDir()
	store := NewMemoryObjectStore(dir)
	return store, dir
}

func TestMemoryObjectStore_Put(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	tests := []struct {
		Name        string
		Key         string
		Data        []byte
		Opts        *objectstore.PutOptions
		ExpectError error
	}{
		{
			Name:        "basic put",
			Key:         "test-file.txt",
			Data:        []byte("hello world"),
			Opts:        nil,
			ExpectError: nil,
		},
		{
			Name: "put with content type",
			Key:  "image.png",
			Data: []byte("fake image data"),
			Opts: &objectstore.PutOptions{
				ContentType: optional.Set("image/png"),
			},
			ExpectError: nil,
		},
		{
			Name: "put with metadata",
			Key:  "document.pdf",
			Data: []byte("fake pdf data"),
			Opts: &objectstore.PutOptions{
				ContentType: optional.Set("application/pdf"),
				Metadata: optional.Set(map[string]string{
					"author":  "test-user",
					"version": "1.0",
				}),
			},
			ExpectError: nil,
		},
		{
			Name:        "put with nested key",
			Key:         "folder/subfolder/file.txt",
			Data:        []byte("nested content"),
			Opts:        nil,
			ExpectError: nil,
		},
		{
			Name:        "empty key returns error",
			Key:         "",
			Data:        []byte("data"),
			Opts:        nil,
			ExpectError: objectstore.ErrInvalidKey,
		},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()
			store, _ := setupTestStore(t)

			err := store.Put(ctx, tt.Key, bytes.NewReader(tt.Data), tt.Opts)

			if tt.ExpectError != nil {
				require.ErrorIs(t, err, tt.ExpectError)
				return
			}

			require.NoError(t, err)
		})
	}
}

func TestMemoryObjectStore_Put_Overwrite(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	store, _ := setupTestStore(t)

	// Put initial data
	err := store.Put(ctx, "file.txt", bytes.NewReader([]byte("initial")), nil)
	require.NoError(t, err)

	// Overwrite with new data
	err = store.Put(ctx, "file.txt", bytes.NewReader([]byte("updated")), nil)
	require.NoError(t, err)

	// Verify overwritten
	obj, err := store.Get(ctx, "file.txt", nil)
	require.NoError(t, err)
	defer obj.Body.Close()

	data, err := io.ReadAll(obj.Body)
	require.NoError(t, err)
	require.Equal(t, "updated", string(data))
}

func TestMemoryObjectStore_Get(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	tests := []struct {
		Name         string
		SetupKey     string
		SetupData    []byte
		SetupOpts    *objectstore.PutOptions
		GetKey       string
		ExpectData   []byte
		ExpectError  error
		ExpectFields func(t *testing.T, obj *objectstore.Object)
	}{
		{
			Name:        "get existing object",
			SetupKey:    "test.txt",
			SetupData:   []byte("test content"),
			SetupOpts:   nil,
			GetKey:      "test.txt",
			ExpectData:  []byte("test content"),
			ExpectError: nil,
		},
		{
			Name:        "get non-existent object",
			SetupKey:    "",
			SetupData:   nil,
			SetupOpts:   nil,
			GetKey:      "missing.txt",
			ExpectData:  nil,
			ExpectError: objectstore.ErrNotFound,
		},
		{
			Name:      "get object with metadata",
			SetupKey:  "doc.pdf",
			SetupData: []byte("pdf content"),
			SetupOpts: &objectstore.PutOptions{
				ContentType: optional.Set("application/pdf"),
				Metadata: optional.Set(map[string]string{
					"author": "tester",
				}),
			},
			GetKey:      "doc.pdf",
			ExpectData:  []byte("pdf content"),
			ExpectError: nil,
			ExpectFields: func(t *testing.T, obj *objectstore.Object) {
				t.Helper()
				require.Equal(t, "application/pdf", obj.ContentType)
				require.Equal(t, "tester", obj.Metadata["author"])
				require.Equal(t, int64(11), obj.Size)
			},
		},
		{
			Name:        "get nested object",
			SetupKey:    "a/b/c.txt",
			SetupData:   []byte("nested"),
			SetupOpts:   nil,
			GetKey:      "a/b/c.txt",
			ExpectData:  []byte("nested"),
			ExpectError: nil,
		},
		{
			Name:        "empty key returns error",
			SetupKey:    "",
			SetupData:   nil,
			SetupOpts:   nil,
			GetKey:      "",
			ExpectData:  nil,
			ExpectError: objectstore.ErrInvalidKey,
		},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()
			store, _ := setupTestStore(t)

			// Setup: put the object if specified
			if tt.SetupKey != "" {
				err := store.Put(ctx, tt.SetupKey, bytes.NewReader(tt.SetupData), tt.SetupOpts)
				require.NoError(t, err)
			}

			obj, err := store.Get(ctx, tt.GetKey, nil)

			if tt.ExpectError != nil {
				require.ErrorIs(t, err, tt.ExpectError)
				return
			}

			require.NoError(t, err)
			defer obj.Body.Close()

			data, err := io.ReadAll(obj.Body)
			require.NoError(t, err)
			require.Equal(t, tt.ExpectData, data)

			if tt.ExpectFields != nil {
				tt.ExpectFields(t, obj)
			}
		})
	}
}

func TestMemoryObjectStore_Get_WithRange(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	tests := []struct {
		Name        string
		SetupKey    string
		SetupData   []byte
		GetKey      string
		GetOpts     *objectstore.GetOptions
		ExpectData  []byte
		ExpectError error
	}{
		{
			Name:        "get first 5 bytes",
			SetupKey:    "file.txt",
			SetupData:   []byte("hello world"),
			GetKey:      "file.txt",
			GetOpts:     &objectstore.GetOptions{Offset: optional.Set[int64](0), Length: optional.Set[int64](5)},
			ExpectData:  []byte("hello"),
			ExpectError: nil,
		},
		{
			Name:        "get middle bytes",
			SetupKey:    "file.txt",
			SetupData:   []byte("hello world"),
			GetKey:      "file.txt",
			GetOpts:     &objectstore.GetOptions{Offset: optional.Set[int64](6), Length: optional.Set[int64](5)},
			ExpectData:  []byte("world"),
			ExpectError: nil,
		},
		{
			Name:        "get with length exceeding file size",
			SetupKey:    "file.txt",
			SetupData:   []byte("short"),
			GetKey:      "file.txt",
			GetOpts:     &objectstore.GetOptions{Offset: optional.Set[int64](0), Length: optional.Set[int64](100)},
			ExpectData:  []byte("short"),
			ExpectError: nil,
		},
		{
			Name:        "get with offset at end",
			SetupKey:    "file.txt",
			SetupData:   []byte("hello"),
			GetKey:      "file.txt",
			GetOpts:     &objectstore.GetOptions{Offset: optional.Set[int64](5), Length: optional.Set[int64](10)},
			ExpectData:  []byte(""),
			ExpectError: nil,
		},
		{
			Name:        "get non-existent object with range",
			SetupKey:    "",
			SetupData:   nil,
			GetKey:      "missing.txt",
			GetOpts:     &objectstore.GetOptions{Offset: optional.Set[int64](0), Length: optional.Set[int64](10)},
			ExpectData:  nil,
			ExpectError: objectstore.ErrNotFound,
		},
		{
			Name:        "empty key with range returns error",
			SetupKey:    "",
			SetupData:   nil,
			GetKey:      "",
			GetOpts:     &objectstore.GetOptions{Offset: optional.Set[int64](0), Length: optional.Set[int64](10)},
			ExpectData:  nil,
			ExpectError: objectstore.ErrInvalidKey,
		},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()
			store, _ := setupTestStore(t)

			if tt.SetupKey != "" {
				err := store.Put(ctx, tt.SetupKey, bytes.NewReader(tt.SetupData), nil)
				require.NoError(t, err)
			}

			obj, err := store.Get(ctx, tt.GetKey, tt.GetOpts)

			if tt.ExpectError != nil {
				require.ErrorIs(t, err, tt.ExpectError)
				return
			}

			require.NoError(t, err)
			defer obj.Body.Close()

			data, err := io.ReadAll(obj.Body)
			require.NoError(t, err)
			require.Equal(t, tt.ExpectData, data)
		})
	}
}

func TestMemoryObjectStore_Delete(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	tests := []struct {
		Name        string
		SetupKey    string
		DeleteKey   string
		ExpectError error
	}{
		{
			Name:        "delete existing object",
			SetupKey:    "file.txt",
			DeleteKey:   "file.txt",
			ExpectError: nil,
		},
		{
			Name:        "delete non-existent object",
			SetupKey:    "",
			DeleteKey:   "missing.txt",
			ExpectError: nil, // S3 semantics: delete is idempotent
		},
		{
			Name:        "delete nested object",
			SetupKey:    "a/b/c.txt",
			DeleteKey:   "a/b/c.txt",
			ExpectError: nil,
		},
		{
			Name:        "empty key returns error",
			SetupKey:    "",
			DeleteKey:   "",
			ExpectError: objectstore.ErrInvalidKey,
		},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()
			store, _ := setupTestStore(t)

			if tt.SetupKey != "" {
				err := store.Put(ctx, tt.SetupKey, bytes.NewReader([]byte("data")), nil)
				require.NoError(t, err)
			}

			err := store.Delete(ctx, tt.DeleteKey)

			if tt.ExpectError != nil {
				require.ErrorIs(t, err, tt.ExpectError)
				return
			}

			require.NoError(t, err)

			// Verify object is gone
			if tt.SetupKey != "" && tt.SetupKey == tt.DeleteKey {
				_, err := store.Get(ctx, tt.DeleteKey, nil)
				require.ErrorIs(t, err, objectstore.ErrNotFound)
			}
		})
	}
}

func TestMemoryObjectStore_Head(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	tests := []struct {
		Name         string
		SetupKey     string
		SetupData    []byte
		SetupOpts    *objectstore.PutOptions
		HeadKey      string
		ExpectError  error
		ExpectFields func(t *testing.T, info *objectstore.ObjectInfo)
	}{
		{
			Name:        "head existing object",
			SetupKey:    "file.txt",
			SetupData:   []byte("hello world"),
			SetupOpts:   nil,
			HeadKey:     "file.txt",
			ExpectError: nil,
			ExpectFields: func(t *testing.T, info *objectstore.ObjectInfo) {
				t.Helper()
				require.Equal(t, "file.txt", info.Key)
				require.Equal(t, int64(11), info.Size)
				require.False(t, info.LastModified.IsZero())
			},
		},
		{
			Name:      "head object with metadata",
			SetupKey:  "doc.pdf",
			SetupData: []byte("pdf data"),
			SetupOpts: &objectstore.PutOptions{
				ContentType: optional.Set("application/pdf"),
				Metadata: optional.Set(map[string]string{
					"author":  "test",
					"version": "2.0",
				}),
			},
			HeadKey:     "doc.pdf",
			ExpectError: nil,
			ExpectFields: func(t *testing.T, info *objectstore.ObjectInfo) {
				t.Helper()
				require.Equal(t, "doc.pdf", info.Key)
				require.Equal(t, "application/pdf", info.ContentType)
				require.Equal(t, "test", info.Metadata["author"])
				require.Equal(t, "2.0", info.Metadata["version"])
			},
		},
		{
			Name:        "head non-existent object",
			SetupKey:    "",
			SetupData:   nil,
			SetupOpts:   nil,
			HeadKey:     "missing.txt",
			ExpectError: objectstore.ErrNotFound,
		},
		{
			Name:        "empty key returns error",
			SetupKey:    "",
			SetupData:   nil,
			SetupOpts:   nil,
			HeadKey:     "",
			ExpectError: objectstore.ErrInvalidKey,
		},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()
			store, _ := setupTestStore(t)

			if tt.SetupKey != "" {
				err := store.Put(ctx, tt.SetupKey, bytes.NewReader(tt.SetupData), tt.SetupOpts)
				require.NoError(t, err)
			}

			info, err := store.Head(ctx, tt.HeadKey)

			if tt.ExpectError != nil {
				require.ErrorIs(t, err, tt.ExpectError)
				return
			}

			require.NoError(t, err)
			if tt.ExpectFields != nil {
				tt.ExpectFields(t, info)
			}
		})
	}
}

func TestMemoryObjectStore_List(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	tests := []struct {
		Name        string
		SetupKeys   []string
		Prefix      string
		ExpectKeys  []string
		ExpectError error
	}{
		{
			Name:        "list all objects",
			SetupKeys:   []string{"a.txt", "b.txt", "c.txt"},
			Prefix:      "",
			ExpectKeys:  []string{"a.txt", "b.txt", "c.txt"},
			ExpectError: nil,
		},
		{
			Name:        "list with prefix",
			SetupKeys:   []string{"docs/a.txt", "docs/b.txt", "images/c.png"},
			Prefix:      "docs/",
			ExpectKeys:  []string{"docs/a.txt", "docs/b.txt"},
			ExpectError: nil,
		},
		{
			Name:        "list with no matches",
			SetupKeys:   []string{"a.txt", "b.txt"},
			Prefix:      "missing/",
			ExpectKeys:  []string{},
			ExpectError: nil,
		},
		{
			Name:        "list empty store",
			SetupKeys:   []string{},
			Prefix:      "",
			ExpectKeys:  []string{},
			ExpectError: nil,
		},
		{
			Name:        "list nested objects",
			SetupKeys:   []string{"a/b/c.txt", "a/b/d.txt", "a/e.txt", "f.txt"},
			Prefix:      "a/b/",
			ExpectKeys:  []string{"a/b/c.txt", "a/b/d.txt"},
			ExpectError: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()
			store, _ := setupTestStore(t)

			for _, key := range tt.SetupKeys {
				err := store.Put(ctx, key, bytes.NewReader([]byte("data")), nil)
				require.NoError(t, err)
			}

			infos, err := store.List(ctx, tt.Prefix)

			if tt.ExpectError != nil {
				require.ErrorIs(t, err, tt.ExpectError)
				return
			}

			require.NoError(t, err)

			keys := make([]string, len(infos))
			for i, info := range infos {
				keys[i] = info.Key
			}

			require.Len(t, keys, len(tt.ExpectKeys))
			for _, expectedKey := range tt.ExpectKeys {
				require.Contains(t, keys, expectedKey)
			}
		})
	}
}

func TestMemoryObjectStore_Copy(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	tests := []struct {
		Name        string
		SetupKey    string
		SetupData   []byte
		SetupOpts   *objectstore.PutOptions
		SrcKey      string
		DstKey      string
		ExpectError error
		ExpectHead  func(t *testing.T, info *objectstore.ObjectInfo)
	}{
		{
			Name:        "copy existing object",
			SetupKey:    "source.txt",
			SetupData:   []byte("original content"),
			SetupOpts:   nil,
			SrcKey:      "source.txt",
			DstKey:      "destination.txt",
			ExpectError: nil,
		},
		{
			Name:      "copy preserves metadata",
			SetupKey:  "source.pdf",
			SetupData: []byte("pdf content"),
			SetupOpts: &objectstore.PutOptions{
				ContentType: optional.Set("application/pdf"),
				Metadata: optional.Set(map[string]string{
					"author":  "tester",
					"version": "1.0",
				}),
			},
			SrcKey:      "source.pdf",
			DstKey:      "copy.pdf",
			ExpectError: nil,
			ExpectHead: func(t *testing.T, info *objectstore.ObjectInfo) {
				t.Helper()
				require.Equal(t, "application/pdf", info.ContentType)
				require.Equal(t, "tester", info.Metadata["author"])
				require.Equal(t, "1.0", info.Metadata["version"])
			},
		},
		{
			Name:        "copy non-existent source",
			SetupKey:    "",
			SetupData:   nil,
			SetupOpts:   nil,
			SrcKey:      "missing.txt",
			DstKey:      "destination.txt",
			ExpectError: objectstore.ErrNotFound,
		},
		{
			Name:        "copy to nested destination",
			SetupKey:    "source.txt",
			SetupData:   []byte("data"),
			SetupOpts:   nil,
			SrcKey:      "source.txt",
			DstKey:      "a/b/c/destination.txt",
			ExpectError: nil,
		},
		{
			Name:        "empty source key returns error",
			SetupKey:    "",
			SetupData:   nil,
			SetupOpts:   nil,
			SrcKey:      "",
			DstKey:      "destination.txt",
			ExpectError: objectstore.ErrInvalidKey,
		},
		{
			Name:        "empty destination key returns error",
			SetupKey:    "source.txt",
			SetupData:   []byte("data"),
			SetupOpts:   nil,
			SrcKey:      "source.txt",
			DstKey:      "",
			ExpectError: objectstore.ErrInvalidKey,
		},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()
			store, _ := setupTestStore(t)

			if tt.SetupKey != "" {
				err := store.Put(ctx, tt.SetupKey, bytes.NewReader(tt.SetupData), tt.SetupOpts)
				require.NoError(t, err)
			}

			err := store.Copy(ctx, tt.SrcKey, tt.DstKey)

			if tt.ExpectError != nil {
				require.ErrorIs(t, err, tt.ExpectError)
				return
			}

			require.NoError(t, err)

			// Verify destination exists with same content
			dstObj, err := store.Get(ctx, tt.DstKey, nil)
			require.NoError(t, err)
			defer dstObj.Body.Close()

			dstData, err := io.ReadAll(dstObj.Body)
			require.NoError(t, err)
			require.Equal(t, tt.SetupData, dstData)

			// Verify source still exists
			srcObj, err := store.Get(ctx, tt.SrcKey, nil)
			require.NoError(t, err)
			defer srcObj.Body.Close()

			if tt.ExpectHead != nil {
				info, err := store.Head(ctx, tt.DstKey)
				require.NoError(t, err)
				tt.ExpectHead(t, info)
			}
		})
	}
}

func TestMemoryObjectStore_LastModified(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	store, _ := setupTestStore(t)

	before := time.Now()
	err := store.Put(ctx, "file.txt", bytes.NewReader([]byte("data")), nil)
	require.NoError(t, err)
	after := time.Now()

	info, err := store.Head(ctx, "file.txt")
	require.NoError(t, err)

	require.True(t, info.LastModified.After(before) || info.LastModified.Equal(before))
	require.True(t, info.LastModified.Before(after) || info.LastModified.Equal(after))
}

func TestMemoryObjectStore_FilesOnDisk(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	store, dir := setupTestStore(t)

	// Put an object
	err := store.Put(ctx, "test/file.txt", bytes.NewReader([]byte("content")), &objectstore.PutOptions{
		ContentType: optional.Set("text/plain"),
	})
	require.NoError(t, err)

	// Verify file exists on disk
	filePath := filepath.Join(dir, "test", "file.txt")
	_, err = os.Stat(filePath)
	require.NoError(t, err)

	// Verify metadata file exists
	metaPath := filepath.Join(dir, "test", "file.txt.meta.json")
	_, err = os.Stat(metaPath)
	require.NoError(t, err)
}
