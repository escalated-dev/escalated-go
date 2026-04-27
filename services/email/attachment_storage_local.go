package email

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

// LocalFileStorage is a reference AttachmentStorage that writes to the
// local filesystem under a configured root directory. Files are
// prefixed with a UTC timestamp to avoid collisions between uploads
// with the same original filename.
//
// Host apps with durable storage needs should provide their own
// AttachmentStorage (S3, GCS, Azure Blob, etc.) and pass it to
// NewAttachmentDownloader instead.
type LocalFileStorage struct {
	Root string
}

// NewLocalFileStorage ensures the root directory exists and returns a
// configured storage. Directory creation is idempotent.
func NewLocalFileStorage(root string) (*LocalFileStorage, error) {
	if root == "" {
		return nil, fmt.Errorf("local file storage root is required")
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, fmt.Errorf("create storage root %s: %w", root, err)
	}
	return &LocalFileStorage{Root: root}, nil
}

// Put writes the content to `{Root}/{YYYYMMDDHHMMSS-nanos}-{filename}`
// and returns the absolute path. Overwrites are vanishingly unlikely
// because the prefix includes nanosecond resolution.
func (s *LocalFileStorage) Put(_ context.Context, filename string, content io.Reader, _ string) (string, error) {
	now := time.Now().UTC()
	prefix := now.Format("20060102150405") + fmt.Sprintf("-%09d", now.Nanosecond())
	storedName := prefix + "-" + filename
	fullPath := filepath.Join(s.Root, storedName)

	f, err := os.Create(fullPath)
	if err != nil {
		return "", fmt.Errorf("create file %s: %w", fullPath, err)
	}
	defer f.Close()

	if _, err := io.Copy(f, content); err != nil {
		// Best-effort cleanup on partial write.
		_ = os.Remove(fullPath)
		return "", fmt.Errorf("write file %s: %w", fullPath, err)
	}
	return fullPath, nil
}
