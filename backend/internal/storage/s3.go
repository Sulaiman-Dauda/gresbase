package storage

import (
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"path/filepath"
	"time"

	"github.com/gresbase/gresbase/internal/config"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// S3Storage handles file storage on S3-compatible services.
type S3Storage struct {
	client *minio.Client
	bucket string
}

// NewS3Storage creates a new S3 storage backend.
func NewS3Storage(cfg *config.Config) (*S3Storage, error) {
	if cfg.S3Endpoint == "" || cfg.S3Bucket == "" {
		return nil, fmt.Errorf("S3 endpoint and bucket are required")
	}

	useSSL := cfg.S3UseSSL
	client, err := minio.New(cfg.S3Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.S3AccessKey, cfg.S3SecretKey, ""),
		Secure: useSSL,
		Region: cfg.S3Region,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create S3 client: %w", err)
	}

	// Ensure bucket exists
	exists, err := client.BucketExists(context.Background(), cfg.S3Bucket)
	if err != nil {
		return nil, fmt.Errorf("failed to check bucket: %w", err)
	}
	if !exists {
		if err := client.MakeBucket(context.Background(), cfg.S3Bucket, minio.MakeBucketOptions{
			Region: cfg.S3Region,
		}); err != nil {
			return nil, fmt.Errorf("failed to create bucket: %w", err)
		}
	}

	return &S3Storage{client: client, bucket: cfg.S3Bucket}, nil
}

// Put uploads a file to S3.
func (s *S3Storage) Put(path string, data []byte, contentType string) error {
	reader := &bytesReader{data: data}
	_, err := s.client.PutObject(context.Background(), s.bucket, path,
		reader, int64(len(data)), minio.PutObjectOptions{
			ContentType: contentType,
		})
	return err
}

// Get retrieves a file from S3.
func (s *S3Storage) Get(path string) ([]byte, *FileInfo, error) {
	obj, err := s.client.GetObject(context.Background(), s.bucket, path, minio.GetObjectOptions{})
	if err != nil {
		return nil, nil, err
	}
	defer obj.Close()

	stat, err := obj.Stat()
	if err != nil {
		return nil, nil, err
	}

	data, err := io.ReadAll(obj)
	if err != nil {
		return nil, nil, err
	}

	info := &FileInfo{
		Name:      filepath.Base(path),
		Path:      path,
		Size:      stat.Size,
		MimeType:  stat.ContentType,
		CreatedAt: stat.LastModified,
	}

	return data, info, nil
}

// Delete removes a file from S3.
func (s *S3Storage) Delete(path string) error {
	return s.client.RemoveObject(context.Background(), s.bucket, path, minio.RemoveObjectOptions{})
}

// DeletePrefix removes all files under the provided S3 prefix.
func (s *S3Storage) DeletePrefix(ctx context.Context, prefix string) error {
	for object := range s.client.ListObjects(ctx, s.bucket, minio.ListObjectsOptions{Prefix: prefix, Recursive: true}) {
		if object.Err != nil {
			return object.Err
		}
		if err := s.client.RemoveObject(ctx, s.bucket, object.Key, minio.RemoveObjectOptions{}); err != nil {
			return err
		}
	}
	return nil
}

// Exists checks if a file exists in S3.
func (s *S3Storage) Exists(path string) bool {
	_, err := s.client.StatObject(context.Background(), s.bucket, path, minio.StatObjectOptions{})
	return err == nil
}

// List returns files with a prefix.
func (s *S3Storage) List(prefix string) ([]*FileInfo, error) {
	objects := s.client.ListObjects(context.Background(), s.bucket, minio.ListObjectsOptions{
		Prefix:    prefix,
		Recursive: true,
	})

	var files []*FileInfo
	for obj := range objects {
		if obj.Err != nil {
			continue
		}
		files = append(files, &FileInfo{
			Name:      filepath.Base(obj.Key),
			Path:      obj.Key,
			Size:      obj.Size,
			MimeType:  obj.ContentType,
			CreatedAt: obj.LastModified,
		})
	}
	return files, nil
}

// GetSignedURL generates a presigned URL for the S3 object.
func (s *S3Storage) GetSignedURL(path string, expiry time.Duration) (string, error) {
	u, err := s.client.PresignedGetObject(context.Background(), s.bucket, path, expiry, nil)
	if err != nil {
		return "", err
	}
	return u.String(), nil
}

// bytesReader implements io.Reader for byte slices.
type bytesReader struct {
	data   []byte
	offset int
}

func (r *bytesReader) Read(p []byte) (int, error) {
	if r.offset >= len(r.data) {
		return 0, io.EOF
	}
	n := copy(p, r.data[r.offset:])
	r.offset += n
	return n, nil
}

// -------------------------------------------------------------------
// Service S3 integration
// -------------------------------------------------------------------

// Ensure multipart import compiles
var _ = multipart.FileHeader{}
