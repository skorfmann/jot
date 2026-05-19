package storage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/skorfmann/jot/internal/manifest"
)

type S3Store struct {
	client   *s3.Client
	presign  *s3.PresignClient
	bucket   string
	endpoint string
}

func NewS3(ctx context.Context, cfg Config) (*S3Store, error) {
	if cfg.Bucket == "" {
		return nil, errors.New("storage.bucket is required")
	}
	if cfg.Region == "" {
		cfg.Region = "auto"
	}
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion(cfg.Region),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(cfg.AccessKeyID, cfg.SecretAccessKey, "")),
	)
	if err != nil {
		return nil, err
	}
	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		o.UsePathStyle = cfg.ForcePathStyle
		if cfg.Endpoint != "" {
			o.BaseEndpoint = aws.String(cfg.Endpoint)
		}
	})
	return &S3Store{
		client:   client,
		presign:  s3.NewPresignClient(client),
		bucket:   cfg.Bucket,
		endpoint: cfg.Endpoint,
	}, nil
}

func (s *S3Store) Health(ctx context.Context) error {
	_, err := s.client.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(s.bucket)})
	return err
}

func (s *S3Store) BlobExists(ctx context.Context, hash string) (bool, error) {
	_, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(BlobKey(hash)),
	})
	if err != nil {
		var nf *types.NotFound
		if errors.As(err, &nf) || isNotFound(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (s *S3Store) PresignPutBlob(ctx context.Context, hash, contentType string, expires time.Duration) (string, error) {
	out, err := s.presign.PresignPutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(s.bucket),
		Key:         aws.String(BlobKey(hash)),
		ContentType: aws.String(contentType),
	}, func(opts *s3.PresignOptions) {
		opts.Expires = expires
	})
	if err != nil {
		return "", err
	}
	return out.URL, nil
}

func (s *S3Store) GetBlob(ctx context.Context, hash string) (io.ReadCloser, BlobMeta, error) {
	out, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(BlobKey(hash)),
	})
	if err != nil {
		if isNotFound(err) {
			return nil, BlobMeta{}, ErrNotFound
		}
		return nil, BlobMeta{}, err
	}
	meta := BlobMeta{
		SHA256:      hash,
		Size:        aws.ToInt64(out.ContentLength),
		ContentType: aws.ToString(out.ContentType),
		ETag:        strings.Trim(aws.ToString(out.ETag), "\""),
	}
	return out.Body, meta, nil
}

func (s *S3Store) PutManifest(ctx context.Context, m *manifest.Manifest) error {
	body, err := manifest.MarshalCanonical(m)
	if err != nil {
		return err
	}
	_, err = s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(s.bucket),
		Key:         aws.String(ManifestKey(m.Slug, m.ID)),
		Body:        bytes.NewReader(body),
		ContentType: aws.String("application/json; charset=utf-8"),
	})
	return err
}

func (s *S3Store) GetManifest(ctx context.Context, slug, id string) (*manifest.Manifest, error) {
	out, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(ManifestKey(slug, id)),
	})
	if err != nil {
		if isNotFound(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	defer out.Body.Close()
	var m manifest.Manifest
	if err := json.NewDecoder(out.Body).Decode(&m); err != nil {
		return nil, err
	}
	return &m, nil
}

func (s *S3Store) ListManifests(ctx context.Context, slug string) ([]*manifest.Manifest, error) {
	return s.listManifestsByPrefix(ctx, "manifests/"+slug+"/")
}

func (s *S3Store) ListAllManifests(ctx context.Context) ([]*manifest.Manifest, error) {
	return s.listManifestsByPrefix(ctx, "manifests/")
}

func (s *S3Store) listManifestsByPrefix(ctx context.Context, prefix string) ([]*manifest.Manifest, error) {
	var manifests []*manifest.Manifest
	p := s3.NewListObjectsV2Paginator(s.client, &s3.ListObjectsV2Input{
		Bucket: aws.String(s.bucket),
		Prefix: aws.String(prefix),
	})
	for p.HasMorePages() {
		page, err := p.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, obj := range page.Contents {
			key := aws.ToString(obj.Key)
			if !strings.HasSuffix(key, ".json") {
				continue
			}
			parts := strings.Split(key, "/")
			if len(parts) != 3 {
				continue
			}
			id := strings.TrimSuffix(parts[2], ".json")
			m, err := s.GetManifest(ctx, parts[1], id)
			if err != nil {
				return nil, err
			}
			manifests = append(manifests, m)
		}
	}
	sort.Slice(manifests, func(i, j int) bool {
		return manifests[i].CreatedAt.After(manifests[j].CreatedAt)
	})
	return manifests, nil
}

func (s *S3Store) GetCurrent(ctx context.Context, slug string) (CurrentRef, error) {
	out, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(CurrentKey(slug)),
	})
	if err != nil {
		if isNotFound(err) {
			return CurrentRef{}, nil
		}
		return CurrentRef{}, err
	}
	defer out.Body.Close()
	var ptr manifest.CurrentPointer
	if err := json.NewDecoder(out.Body).Decode(&ptr); err != nil {
		return CurrentRef{}, err
	}
	return CurrentRef{
		Pointer: ptr,
		ETag:    aws.ToString(out.ETag),
		Found:   true,
	}, nil
}

func (s *S3Store) PutCurrent(ctx context.Context, slug string, ptr manifest.CurrentPointer, ifMatch *string, ifNoneMatch bool) error {
	body, err := json.Marshal(ptr)
	if err != nil {
		return err
	}
	input := &s3.PutObjectInput{
		Bucket:      aws.String(s.bucket),
		Key:         aws.String(CurrentKey(slug)),
		Body:        bytes.NewReader(body),
		ContentType: aws.String("application/json; charset=utf-8"),
	}
	if ifMatch != nil {
		input.IfMatch = ifMatch
	}
	if ifNoneMatch {
		input.IfNoneMatch = aws.String("*")
	}
	_, err = s.client.PutObject(ctx, input)
	return err
}

func (s *S3Store) DeleteSlug(ctx context.Context, slug string) error {
	keys := []types.ObjectIdentifier{{Key: aws.String(CurrentKey(slug))}}
	p := s3.NewListObjectsV2Paginator(s.client, &s3.ListObjectsV2Input{
		Bucket: aws.String(s.bucket),
		Prefix: aws.String("manifests/" + slug + "/"),
	})
	for p.HasMorePages() {
		page, err := p.NextPage(ctx)
		if err != nil {
			return err
		}
		for _, obj := range page.Contents {
			keys = append(keys, types.ObjectIdentifier{Key: obj.Key})
		}
	}
	return s.deleteKeys(ctx, keys)
}

func (s *S3Store) PruneHistory(ctx context.Context, slug string, keep int) error {
	if keep <= 0 {
		return nil
	}
	items, err := s.ListManifests(ctx, slug)
	if err != nil {
		return err
	}
	if len(items) <= keep {
		return nil
	}
	keys := make([]types.ObjectIdentifier, 0, len(items)-keep)
	for _, item := range items[keep:] {
		keys = append(keys, types.ObjectIdentifier{Key: aws.String(ManifestKey(item.Slug, item.ID))})
	}
	return s.deleteKeys(ctx, keys)
}

func (s *S3Store) ListBlobHashes(ctx context.Context) (map[string]struct{}, error) {
	hashes := map[string]struct{}{}
	p := s3.NewListObjectsV2Paginator(s.client, &s3.ListObjectsV2Input{
		Bucket: aws.String(s.bucket),
		Prefix: aws.String("blobs/sha256/"),
	})
	for p.HasMorePages() {
		page, err := p.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, obj := range page.Contents {
			hash := strings.TrimPrefix(aws.ToString(obj.Key), "blobs/sha256/")
			if len(hash) == 64 {
				hashes[hash] = struct{}{}
			}
		}
	}
	return hashes, nil
}

func (s *S3Store) MoveBlobToTrash(ctx context.Context, hash string) error {
	source := s.bucket + "/" + BlobKey(hash)
	_, err := s.client.CopyObject(ctx, &s3.CopyObjectInput{
		Bucket:     aws.String(s.bucket),
		CopySource: aws.String(source),
		Key:        aws.String("_trash/" + hash),
	})
	if err != nil {
		if isNotFound(err) {
			return nil
		}
		return err
	}
	_, err = s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(BlobKey(hash)),
	})
	return err
}

func (s *S3Store) DeleteExpiredTrash(ctx context.Context, ttl time.Duration) error {
	cutoff := time.Now().Add(-ttl)
	var keys []types.ObjectIdentifier
	p := s3.NewListObjectsV2Paginator(s.client, &s3.ListObjectsV2Input{
		Bucket: aws.String(s.bucket),
		Prefix: aws.String("_trash/"),
	})
	for p.HasMorePages() {
		page, err := p.NextPage(ctx)
		if err != nil {
			return err
		}
		for _, obj := range page.Contents {
			if obj.LastModified != nil && obj.LastModified.Before(cutoff) {
				keys = append(keys, types.ObjectIdentifier{Key: obj.Key})
			}
		}
	}
	return s.deleteKeys(ctx, keys)
}

func (s *S3Store) deleteKeys(ctx context.Context, keys []types.ObjectIdentifier) error {
	for len(keys) > 0 {
		n := len(keys)
		if n > 1000 {
			n = 1000
		}
		batch := keys[:n]
		keys = keys[n:]
		_, err := s.client.DeleteObjects(ctx, &s3.DeleteObjectsInput{
			Bucket: aws.String(s.bucket),
			Delete: &types.Delete{Objects: batch, Quiet: aws.Bool(true)},
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func isNotFound(err error) bool {
	if err == nil {
		return false
	}
	var nf *types.NotFound
	if errors.As(err, &nf) {
		return true
	}
	var apiErr interface{ ErrorCode() string }
	if errors.As(err, &apiErr) {
		code := apiErr.ErrorCode()
		return code == "NoSuchKey" || code == "NotFound" || code == "404"
	}
	return strings.Contains(err.Error(), "status code: 404")
}

func IsPreconditionFailed(err error) bool {
	if err == nil {
		return false
	}
	var apiErr interface {
		ErrorCode() string
		HTTPStatusCode() int
	}
	if errors.As(err, &apiErr) {
		return apiErr.HTTPStatusCode() == http.StatusPreconditionFailed || apiErr.ErrorCode() == "PreconditionFailed"
	}
	return strings.Contains(err.Error(), "PreconditionFailed") || strings.Contains(err.Error(), "status code: 412")
}

func IsNotFound(err error) bool {
	return errors.Is(err, ErrNotFound) || isNotFound(err)
}

func FormatLimit(n int64) string {
	if n < 1024 {
		return fmt.Sprintf("%d B", n)
	}
	if n < 1024*1024 {
		return fmt.Sprintf("%.1f KB", float64(n)/1024)
	}
	return fmt.Sprintf("%.1f MB", float64(n)/(1024*1024))
}
