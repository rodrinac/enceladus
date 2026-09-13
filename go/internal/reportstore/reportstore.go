// Package reportstore persists generated report PDFs so they survive
// restarts. When ENCELADUS_REPORTS_BUCKET is set the S3 implementation is
// used; otherwise reports stay on the local filesystem (dev/test).
package reportstore

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// Storage is the persistence boundary for report PDFs.
type Storage interface {
	Exists(ctx context.Context, key string) (bool, error)
	Get(ctx context.Context, key string) ([]byte, error)
	Put(ctx context.Context, key string, data []byte, contentType string) error
	List(ctx context.Context, prefix string) ([]string, error)
}

// KeyForReport maps a config report path ("/relatorios/queimaduras/...") and a
// PDF file name to a storage key ("queimaduras/.../FILE.pdf").
func KeyForReport(reportPath, fileName string) string {
	subdir := strings.TrimPrefix(strings.TrimPrefix(reportPath, "/"), "relatorios/")
	subdir = strings.Trim(subdir, "/")
	if subdir == "" {
		return fileName
	}
	return subdir + "/" + fileName
}

// PublicURI keeps the existing download URL shape for a stored key.
func PublicURI(reportPath, fileName string) string {
	return strings.TrimSuffix(reportPath, "/") + "/" + fileName
}

// Local keeps PDFs under Dir using the same relative layout as the keys.
type Local struct {
	Dir string
}

func (l *Local) path(key string) string {
	return filepath.Join(l.Dir, filepath.FromSlash(key))
}

func (l *Local) Exists(_ context.Context, key string) (bool, error) {
	_, err := os.Stat(l.path(key))
	if err == nil {
		return true, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return false, err
}

func (l *Local) Get(_ context.Context, key string) ([]byte, error) {
	return os.ReadFile(l.path(key))
}

func (l *Local) Put(_ context.Context, key string, data []byte, _ string) error {
	path := l.path(key)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func (l *Local) List(_ context.Context, prefix string) ([]string, error) {
	base := l.path(prefix)
	matches, err := filepath.Glob(filepath.Join(base, "*.pdf"))
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(matches))
	for _, match := range matches {
		rel, err := filepath.Rel(l.Dir, match)
		if err != nil {
			continue
		}
		out = append(out, filepath.ToSlash(rel))
	}
	sort.Strings(out)
	return out, nil
}

// Memory is an in-process Storage for tests.
type Memory struct {
	mu   sync.Mutex
	keys map[string][]byte
}

func NewMemory() *Memory {
	return &Memory{keys: make(map[string][]byte)}
}

func (m *Memory) Exists(_ context.Context, key string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.keys[key]
	return ok, nil
}

func (m *Memory) Get(_ context.Context, key string) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	data, ok := m.keys[key]
	if !ok {
		return nil, os.ErrNotExist
	}
	return data, nil
}

func (m *Memory) Put(_ context.Context, key string, data []byte, _ string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]byte, len(data))
	copy(cp, data)
	m.keys[key] = cp
	return nil
}

func (m *Memory) List(_ context.Context, prefix string) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []string
	for key := range m.keys {
		if strings.HasPrefix(key, prefix) && strings.HasSuffix(key, ".pdf") {
			out = append(out, key)
		}
	}
	sort.Strings(out)
	return out, nil
}

// S3 persists PDFs in a bucket so files survive restarts and instance
// replacement. Keys are prefixed so one bucket can host several environments.
type S3 struct {
	Client *s3.Client
	Bucket string
	Prefix string
}

func (s *S3) fullKey(key string) string {
	prefix := strings.Trim(s.Prefix, "/")
	if prefix == "" {
		return key
	}
	return prefix + "/" + key
}

func (s *S3) Exists(ctx context.Context, key string) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	_, err := s.Client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(s.Bucket),
		Key:    aws.String(s.fullKey(key)),
	})
	if err == nil {
		return true, nil
	}
	var notFound *types.NotFound
	if errors.As(err, &notFound) {
		return false, nil
	}
	// HeadObject surfaces missing keys as a generic 404 API error.
	var apiErr interface{ ErrorCode() string }
	if errors.As(err, &apiErr) && apiErr.ErrorCode() == "NotFound" {
		return false, nil
	}
	if strings.Contains(err.Error(), "NotFound") || strings.Contains(err.Error(), "404") {
		return false, nil
	}
	return false, err
}

func (s *S3) Get(ctx context.Context, key string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	out, err := s.Client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.Bucket),
		Key:    aws.String(s.fullKey(key)),
	})
	if err != nil {
		return nil, err
	}
	defer out.Body.Close()
	return io.ReadAll(out.Body)
}

func (s *S3) Put(ctx context.Context, key string, data []byte, contentType string) error {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	if contentType == "" {
		contentType = "application/pdf"
	}
	_, err := s.Client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(s.Bucket),
		Key:         aws.String(s.fullKey(key)),
		Body:        bytes.NewReader(data),
		ContentType: aws.String(contentType),
	})
	return err
}

func (s *S3) List(ctx context.Context, prefix string) ([]string, error) {
	fullPrefix := s.fullKey(prefix)
	if !strings.HasSuffix(fullPrefix, "/") {
		fullPrefix += "/"
	}
	paginator := s3.NewListObjectsV2Paginator(s.Client, &s3.ListObjectsV2Input{
		Bucket: aws.String(s.Bucket),
		Prefix: aws.String(fullPrefix),
	})
	var out []string
	trimmedPrefix := strings.Trim(s.Prefix, "/")
	for paginator.HasMorePages() {
		ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		page, err := paginator.NextPage(ctx)
		cancel()
		if err != nil {
			return nil, err
		}
		for _, obj := range page.Contents {
			if obj.Key == nil || !strings.HasSuffix(*obj.Key, ".pdf") {
				continue
			}
			key := *obj.Key
			if trimmedPrefix != "" {
				key = strings.TrimPrefix(strings.TrimPrefix(key, trimmedPrefix), "/")
			}
			out = append(out, key)
		}
	}
	sort.Strings(out)
	return out, nil
}
