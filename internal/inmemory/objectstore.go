package inmemory

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"

	"nautilus/internal/errors"
	"nautilus/internal/objectstore"
)

var _ objectstore.Store = new(MemoryObjectStore)

type MemoryObjectStore struct {
	basePath string
}

type objectMeta struct {
	Key         string            `json:"key"`
	Size        int64             `json:"size"`
	ContentType string            `json:"content_type"`
	Metadata    map[string]string `json:"metadata"`
}

func NewMemoryObjectStore(basePath string) *MemoryObjectStore {
	return &MemoryObjectStore{
		basePath: basePath,
	}
}

func (s *MemoryObjectStore) Put(_ context.Context, key string, data io.Reader, opts *objectstore.PutOptions) error {
	if key == "" {
		return objectstore.ErrInvalidKey
	}

	filePath := filepath.Join(s.basePath, key)

	// Create parent directories if needed
	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return errors.Wrapf(err, "failed to create directory: %s", dir)
	}

	// Create the file
	f, err := os.Create(filePath)
	if err != nil {
		return errors.Wrapf(err, "failed to create file: %s", filePath)
	}
	defer f.Close()

	// Write data
	size, err := io.Copy(f, data)
	if err != nil {
		return errors.Wrapf(err, "failed to write data to file: %s", filePath)
	}

	// Prepare metadata
	meta := objectMeta{
		Key:  key,
		Size: size,
	}

	if opts != nil {
		if opts.ContentType.Set {
			meta.ContentType = opts.ContentType.Data
		}
		if opts.Metadata.Set {
			meta.Metadata = opts.Metadata.Data
		}
	}

	// Write metadata file
	metaPath := filePath + ".meta.json"
	metaFile, err := os.Create(metaPath)
	if err != nil {
		return errors.Wrapf(err, "failed to create metadata file: %s", metaPath)
	}
	defer metaFile.Close()

	if err := json.NewEncoder(metaFile).Encode(meta); err != nil {
		return errors.Wrapf(err, "failed to write metadata: %s", metaPath)
	}

	return nil
}

func (s *MemoryObjectStore) Get(_ context.Context, key string, opts *objectstore.GetOptions) (*objectstore.Object, error) {
	if key == "" {
		return nil, objectstore.ErrInvalidKey
	}

	filePath := filepath.Join(s.basePath, key)

	// Check if file exists
	stat, err := os.Stat(filePath)
	if os.IsNotExist(err) {
		return nil, objectstore.ErrNotFound
	}
	if err != nil {
		return nil, errors.Wrapf(err, "failed to stat file: %s", filePath)
	}

	// Open the file
	f, err := os.Open(filePath)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to open file: %s", filePath)
	}

	// Read metadata
	meta, err := s.readMeta(filePath)
	if err != nil {
		f.Close()
		return nil, err
	}

	// Handle range options
	var body io.ReadCloser = f
	if opts != nil {
		// Seek to offset if specified
		if opts.Offset.Set && opts.Offset.Data > 0 {
			_, err = f.Seek(opts.Offset.Data, io.SeekStart)
			if err != nil {
				f.Close()
				return nil, errors.Wrapf(err, "failed to seek to offset %d", opts.Offset.Data)
			}
		}

		// Limit read length if specified
		if opts.Length.Set && opts.Length.Data > 0 {
			body = &limitedReadCloser{
				r:      io.LimitReader(f, opts.Length.Data),
				closer: f,
			}
		}
	}

	return &objectstore.Object{
		ObjectInfo: objectstore.ObjectInfo{
			Key:          key,
			Size:         meta.Size,
			LastModified: stat.ModTime(),
			ContentType:  meta.ContentType,
			Metadata:     meta.Metadata,
		},
		Body: body,
	}, nil
}

type limitedReadCloser struct {
	r      io.Reader
	closer io.Closer
}

func (l *limitedReadCloser) Read(p []byte) (n int, err error) {
	n, err = l.r.Read(p)
	if err == nil {
		return n, nil
	}
	// Don't wrap io.EOF - it's a sentinel that callers check for.
	if err == io.EOF {
		return n, io.EOF
	}
	return n, errors.Wrap(err, "failed to read from limited read closer")
}

func (l *limitedReadCloser) Close() error {
	err := l.closer.Close()
	if err != nil {
		return errors.Wrap(err, "failed to close limited read closer")
	}
	return nil
}

func (s *MemoryObjectStore) Delete(_ context.Context, key string) error {
	if key == "" {
		return objectstore.ErrInvalidKey
	}

	filePath := filepath.Join(s.basePath, key)
	metaPath := filePath + ".meta.json"

	// Remove the file (ignore not exist errors - S3 semantics)
	err := os.Remove(filePath)
	if err != nil && !os.IsNotExist(err) {
		return errors.Wrapf(err, "failed to delete file: %s", filePath)
	}

	// Remove the metadata file
	err = os.Remove(metaPath)
	if err != nil && !os.IsNotExist(err) {
		return errors.Wrapf(err, "failed to delete metadata file: %s", metaPath)
	}

	return nil
}

func (s *MemoryObjectStore) Head(_ context.Context, key string) (*objectstore.ObjectInfo, error) {
	if key == "" {
		return nil, objectstore.ErrInvalidKey
	}

	filePath := filepath.Join(s.basePath, key)

	// Check if file exists
	stat, err := os.Stat(filePath)
	if os.IsNotExist(err) {
		return nil, objectstore.ErrNotFound
	}
	if err != nil {
		return nil, errors.Wrapf(err, "failed to stat file: %s", filePath)
	}

	// Read metadata
	meta, err := s.readMeta(filePath)
	if err != nil {
		return nil, err
	}

	return &objectstore.ObjectInfo{
		Key:          key,
		Size:         meta.Size,
		LastModified: stat.ModTime(),
		ContentType:  meta.ContentType,
		Metadata:     meta.Metadata,
	}, nil
}

func (s *MemoryObjectStore) List(_ context.Context, prefix string) ([]objectstore.ObjectInfo, error) {
	var results []objectstore.ObjectInfo

	err := filepath.Walk(s.basePath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip directories and metadata files
		if info.IsDir() || strings.HasSuffix(path, ".meta.json") {
			return nil
		}

		// Get the key (relative path from basePath)
		key, err := filepath.Rel(s.basePath, path)
		if err != nil {
			return errors.Wrapf(err, "failed to get relative path: %s", path)
		}

		// Normalize path separators to forward slashes
		key = filepath.ToSlash(key)

		// Check prefix
		if prefix != "" && !strings.HasPrefix(key, prefix) {
			return nil
		}

		// Read metadata
		meta, err := s.readMeta(path)
		if err != nil {
			// If metadata doesn't exist, use file info
			meta = &objectMeta{
				Key:  key,
				Size: info.Size(),
			}
		}

		results = append(results, objectstore.ObjectInfo{
			Key:          key,
			Size:         meta.Size,
			LastModified: info.ModTime(),
			ContentType:  meta.ContentType,
			Metadata:     meta.Metadata,
		})

		return nil
	})

	if err != nil {
		return nil, errors.Wrapf(err, "failed to list objects")
	}

	if results == nil {
		results = []objectstore.ObjectInfo{}
	}

	return results, nil
}

func (s *MemoryObjectStore) Copy(_ context.Context, srcKey, dstKey string) error {
	if srcKey == "" || dstKey == "" {
		return objectstore.ErrInvalidKey
	}

	srcPath := filepath.Join(s.basePath, srcKey)
	dstPath := filepath.Join(s.basePath, dstKey)

	// Check if source exists
	srcStat, err := os.Stat(srcPath)
	if os.IsNotExist(err) {
		return objectstore.ErrNotFound
	}
	if err != nil {
		return errors.Wrapf(err, "failed to stat source file: %s", srcPath)
	}

	// Create destination directory
	dstDir := filepath.Dir(dstPath)
	if err := os.MkdirAll(dstDir, 0755); err != nil {
		return errors.Wrapf(err, "failed to create directory: %s", dstDir)
	}

	// Copy the file
	srcFile, err := os.Open(srcPath)
	if err != nil {
		return errors.Wrapf(err, "failed to open source file: %s", srcPath)
	}
	defer srcFile.Close()

	dstFile, err := os.Create(dstPath)
	if err != nil {
		return errors.Wrapf(err, "failed to create destination file: %s", dstPath)
	}
	defer dstFile.Close()

	_, err = io.Copy(dstFile, srcFile)
	if err != nil {
		return errors.Wrapf(err, "failed to copy file data")
	}

	// Copy metadata file
	srcMetaPath := srcPath + ".meta.json"
	dstMetaPath := dstPath + ".meta.json"

	srcMeta, err := s.readMeta(srcPath)
	if err != nil {
		// Create basic metadata if none exists
		srcMeta = &objectMeta{
			Key:  dstKey,
			Size: srcStat.Size(),
		}
	} else {
		srcMeta.Key = dstKey
	}

	dstMetaFile, err := os.Create(dstMetaPath)
	if err != nil {
		return errors.Wrapf(err, "failed to create metadata file: %s", dstMetaPath)
	}
	defer dstMetaFile.Close()

	if err := json.NewEncoder(dstMetaFile).Encode(srcMeta); err != nil {
		return errors.Wrapf(err, "failed to write metadata: %s", srcMetaPath)
	}

	return nil
}

func (s *MemoryObjectStore) readMeta(filePath string) (*objectMeta, error) {
	metaPath := filePath + ".meta.json"

	f, err := os.Open(metaPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, errors.Wrapf(err, "metadata file not found: %s", metaPath)
		}
		return nil, errors.Wrapf(err, "failed to open metadata file: %s", metaPath)
	}
	defer f.Close()

	var meta objectMeta
	if err := json.NewDecoder(f).Decode(&meta); err != nil {
		return nil, errors.Wrapf(err, "failed to decode metadata: %s", metaPath)
	}

	return &meta, nil
}
