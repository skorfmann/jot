package storage

import (
	"context"
	"errors"
	"io"
	"time"

	"github.com/skorfmann/jot/internal/manifest"
)

var ErrNotFound = errors.New("not found")

type Config struct {
	Endpoint        string
	Region          string
	Bucket          string
	AccessKeyID     string
	SecretAccessKey string
	ForcePathStyle  bool
}

type BlobMeta struct {
	SHA256      string
	Size        int64
	ContentType string
	ETag        string
}

type UploadRef struct {
	SHA256 string `json:"sha256"`
	URL    string `json:"url"`
}

type CurrentRef struct {
	Pointer manifest.CurrentPointer
	ETag    string
	Found   bool
}

type Store interface {
	Health(context.Context) error
	BlobExists(context.Context, string) (bool, error)
	PresignPutBlob(context.Context, string, string, time.Duration) (string, error)
	GetBlob(context.Context, string) (io.ReadCloser, BlobMeta, error)
	PutManifest(context.Context, *manifest.Manifest) error
	GetManifest(context.Context, string, string) (*manifest.Manifest, error)
	ListManifests(context.Context, string) ([]*manifest.Manifest, error)
	ListAllManifests(context.Context) ([]*manifest.Manifest, error)
	GetCurrent(context.Context, string) (CurrentRef, error)
	PutCurrent(context.Context, string, manifest.CurrentPointer, *string, bool) error
	DeleteSlug(context.Context, string) error
	PruneHistory(context.Context, string, int) error
	ListBlobHashes(context.Context) (map[string]struct{}, error)
	MoveBlobToTrash(context.Context, string) error
	DeleteExpiredTrash(context.Context, time.Duration) error
}

func BlobKey(hash string) string {
	return "blobs/sha256/" + hash
}

func ManifestKey(slug, id string) string {
	return "manifests/" + slug + "/" + id + ".json"
}

func CurrentKey(slug string) string {
	return "slugs/" + slug + "/current"
}
