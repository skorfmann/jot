package storage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"cloud.google.com/go/compute/metadata"
	iamcredentials "cloud.google.com/go/iam/credentials/apiv1"
	"cloud.google.com/go/iam/credentials/apiv1/credentialspb"
	gcs "cloud.google.com/go/storage"
	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/skorfmann/jot/internal/manifest"
	"gocloud.dev/blob"
	"gocloud.dev/blob/gcsblob"
	"gocloud.dev/blob/s3blob"
	"gocloud.dev/gcerrors"
	"gocloud.dev/gcp"
)

type BlobStore struct {
	bucket *blob.Bucket
}

func New(ctx context.Context, cfg Config) (*BlobStore, error) {
	bucket, err := openBucket(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return &BlobStore{bucket: bucket}, nil
}

func openBucket(ctx context.Context, cfg Config) (*blob.Bucket, error) {
	if cfg.URL != "" {
		u, err := url.Parse(cfg.URL)
		if err != nil {
			return nil, err
		}
		if u.Scheme == "gs" {
			return openGCSBucket(ctx, u, cfg.GoogleAccessID)
		}
		return blob.OpenBucket(ctx, cfg.URL)
	}
	if cfg.Bucket == "" {
		return nil, errors.New("storage.bucket or storage.url is required")
	}
	if isGCSEndpoint(cfg.Endpoint) {
		return openGCSBucket(ctx, &url.URL{Scheme: "gs", Host: cfg.Bucket}, cfg.GoogleAccessID)
	}
	return openS3Bucket(ctx, cfg)
}

func openGCSBucket(ctx context.Context, u *url.URL, fallbackAccessID string) (*blob.Bucket, error) {
	if u.Host == "" {
		return nil, errors.New("GCS storage URL must include a bucket, e.g. gs://bucket-name")
	}
	q := u.Query()
	accessID := q.Get("access_id")
	if accessID == "" {
		accessID = fallbackAccessID
	}
	q.Del("access_id")
	if len(q) > 0 {
		return nil, fmt.Errorf("unsupported GCS storage URL query parameter %q", firstQueryKey(q))
	}

	creds, err := gcp.DefaultCredentials(ctx)
	if err != nil {
		return nil, err
	}
	if accessID == "" {
		accessID = googleAccessIDFromCredentials(creds.JSON)
	}
	if accessID == "" && metadata.OnGCEWithContext(ctx) {
		accessID, _ = metadata.EmailWithContext(ctx, "default")
	}

	client, err := gcp.NewHTTPClient(gcp.DefaultTransport(), gcp.CredentialsTokenSource(creds))
	if err != nil {
		return nil, err
	}
	opts := &gcsblob.Options{GoogleAccessID: accessID}
	if accessID != "" {
		makeSignBytes, err := makeIAMSignBytes(ctx, accessID)
		if err != nil {
			return nil, err
		}
		opts.MakeSignBytes = makeSignBytes
	}
	return gcsblob.OpenBucket(ctx, client, u.Host, opts)
}

func openS3Bucket(ctx context.Context, cfg Config) (*blob.Bucket, error) {
	if cfg.Region == "" {
		cfg.Region = "auto"
	}
	loadOptions := []func(*awsconfig.LoadOptions) error{
		awsconfig.WithRegion(cfg.Region),
	}
	if cfg.AccessKeyID != "" || cfg.SecretAccessKey != "" {
		loadOptions = append(loadOptions, awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(cfg.AccessKeyID, cfg.SecretAccessKey, "")))
	}
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, loadOptions...)
	if err != nil {
		return nil, err
	}
	client := awss3.NewFromConfig(awsCfg, func(o *awss3.Options) {
		o.UsePathStyle = cfg.ForcePathStyle
		if cfg.Endpoint != "" {
			o.BaseEndpoint = aws.String(cfg.Endpoint)
			o.RequestChecksumCalculation = aws.RequestChecksumCalculationWhenRequired
			o.ResponseChecksumValidation = aws.ResponseChecksumValidationWhenRequired
		}
	})
	return s3blob.OpenBucket(ctx, client, cfg.Bucket, &s3blob.Options{
		RequestChecksumCalculation: aws.RequestChecksumCalculationWhenRequired,
	})
}

func isGCSEndpoint(endpoint string) bool {
	if endpoint == "" {
		return false
	}
	u, err := url.Parse(endpoint)
	if err != nil {
		return false
	}
	host := u.Host
	if host == "" {
		host = u.Path
	}
	return strings.EqualFold(strings.TrimSuffix(host, "/"), "storage.googleapis.com")
}

func firstQueryKey(values url.Values) string {
	for key := range values {
		return key
	}
	return ""
}

func makeIAMSignBytes(ctx context.Context, accessID string) (func(context.Context) gcsblob.SignBytesFunc, error) {
	client, err := iamcredentials.NewIamCredentialsClient(ctx)
	if err != nil {
		return nil, err
	}
	name := accessID
	if !strings.HasPrefix(name, "projects/") {
		name = "projects/-/serviceAccounts/" + accessID
	}
	return func(requestCtx context.Context) gcsblob.SignBytesFunc {
		return func(payload []byte) ([]byte, error) {
			resp, err := client.SignBlob(requestCtx, &credentialspb.SignBlobRequest{
				Name:    name,
				Payload: payload,
			})
			if err != nil {
				return nil, err
			}
			return resp.SignedBlob, nil
		}
	}, nil
}

func googleAccessIDFromCredentials(body []byte) string {
	var serviceAccount struct {
		ClientEmail string `json:"client_email"`
	}
	if err := json.Unmarshal(body, &serviceAccount); err == nil && serviceAccount.ClientEmail != "" {
		return serviceAccount.ClientEmail
	}
	var key struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(body, &key); err == nil {
		parts := strings.Split(key.Name, "/")
		for i := range parts {
			if parts[i] == "serviceAccounts" && i+1 < len(parts) {
				return parts[i+1]
			}
		}
	}
	return ""
}

func (s *BlobStore) Health(ctx context.Context) error {
	iter := s.bucket.List(&blob.ListOptions{Prefix: ""})
	_, err := iter.Next(ctx)
	if errors.Is(err, io.EOF) {
		return nil
	}
	return err
}

func (s *BlobStore) BlobExists(ctx context.Context, hash string) (bool, error) {
	exists, err := s.bucket.Exists(ctx, BlobKey(hash))
	if err != nil {
		return false, err
	}
	return exists, nil
}

func (s *BlobStore) PresignPutBlob(ctx context.Context, hash, contentType string, expires time.Duration) (string, error) {
	opts := &blob.SignedURLOptions{
		Method:      http.MethodPut,
		Expiry:      expires,
		ContentType: contentType,
	}
	u, err := s.bucket.SignedURL(ctx, BlobKey(hash), opts)
	if err == nil {
		return u, nil
	}
	if contentType != "" && gcerrors.Code(err) == gcerrors.Unimplemented {
		return s.bucket.SignedURL(ctx, BlobKey(hash), &blob.SignedURLOptions{
			Method: http.MethodPut,
			Expiry: expires,
		})
	}
	return "", err
}

func (s *BlobStore) GetBlob(ctx context.Context, hash string) (io.ReadCloser, BlobMeta, error) {
	key := BlobKey(hash)
	r, err := s.bucket.NewReader(ctx, key, nil)
	if err != nil {
		if IsNotFound(err) {
			return nil, BlobMeta{}, ErrNotFound
		}
		return nil, BlobMeta{}, err
	}
	return r, BlobMeta{
		SHA256:      hash,
		Size:        r.Size(),
		ContentType: r.ContentType(),
	}, nil
}

func (s *BlobStore) PutManifest(ctx context.Context, m *manifest.Manifest) error {
	body, err := manifest.MarshalCanonical(m)
	if err != nil {
		return err
	}
	return s.bucket.WriteAll(ctx, ManifestKey(m.Slug, m.ID), body, &blob.WriterOptions{
		ContentType: "application/json; charset=utf-8",
	})
}

func (s *BlobStore) GetManifest(ctx context.Context, slug, id string) (*manifest.Manifest, error) {
	body, err := s.bucket.ReadAll(ctx, ManifestKey(slug, id))
	if err != nil {
		if IsNotFound(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	var m manifest.Manifest
	if err := json.NewDecoder(bytes.NewReader(body)).Decode(&m); err != nil {
		return nil, err
	}
	return &m, nil
}

func (s *BlobStore) ListManifests(ctx context.Context, slug string) ([]*manifest.Manifest, error) {
	return s.listManifestsByPrefix(ctx, "manifests/"+slug+"/")
}

func (s *BlobStore) ListAllManifests(ctx context.Context) ([]*manifest.Manifest, error) {
	return s.listManifestsByPrefix(ctx, "manifests/")
}

func (s *BlobStore) listManifestsByPrefix(ctx context.Context, prefix string) ([]*manifest.Manifest, error) {
	var manifests []*manifest.Manifest
	iter := s.bucket.List(&blob.ListOptions{Prefix: prefix})
	for {
		obj, err := iter.Next(ctx)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		if obj.IsDir || !strings.HasSuffix(obj.Key, ".json") {
			continue
		}
		parts := strings.Split(obj.Key, "/")
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
	sort.Slice(manifests, func(i, j int) bool {
		return manifests[i].CreatedAt.After(manifests[j].CreatedAt)
	})
	return manifests, nil
}

func (s *BlobStore) GetCurrent(ctx context.Context, slug string) (CurrentRef, error) {
	key := CurrentKey(slug)
	attrs, err := s.bucket.Attributes(ctx, key)
	if err != nil {
		if IsNotFound(err) {
			return CurrentRef{}, nil
		}
		return CurrentRef{}, err
	}
	body, err := s.bucket.ReadAll(ctx, key)
	if err != nil {
		if IsNotFound(err) {
			return CurrentRef{}, nil
		}
		return CurrentRef{}, err
	}
	var ptr manifest.CurrentPointer
	if err := json.NewDecoder(bytes.NewReader(body)).Decode(&ptr); err != nil {
		return CurrentRef{}, err
	}
	return CurrentRef{
		Pointer: ptr,
		ETag:    objectRevision(attrs),
		Found:   true,
	}, nil
}

func (s *BlobStore) PutCurrent(ctx context.Context, slug string, ptr manifest.CurrentPointer, ifMatch *string, ifNoneMatch bool) error {
	body, err := json.Marshal(ptr)
	if err != nil {
		return err
	}
	opts := &blob.WriterOptions{
		ContentType: "application/json; charset=utf-8",
		IfNotExist:  ifNoneMatch,
	}
	if ifMatch != nil {
		opts.BeforeWrite = conditionalWrite(*ifMatch)
	}
	return s.bucket.WriteAll(ctx, CurrentKey(slug), body, opts)
}

func conditionalWrite(revision string) func(func(any) bool) error {
	return func(as func(any) bool) error {
		var gcsObj **gcs.ObjectHandle
		if as(&gcsObj) {
			generation, err := strconv.ParseInt(revision, 10, 64)
			if err != nil {
				return fmt.Errorf("invalid GCS generation %q: %w", revision, err)
			}
			*gcsObj = (*gcsObj).If(gcs.Conditions{GenerationMatch: generation})
			return nil
		}
		var put **awss3.PutObjectInput
		if as(&put) {
			(*put).IfMatch = aws.String(revision)
			return nil
		}
		return errors.New("conditional current update is not supported by this storage driver")
	}
}

func objectRevision(attrs *blob.Attributes) string {
	var gcsAttrs gcs.ObjectAttrs
	if attrs.As(&gcsAttrs) && gcsAttrs.Generation > 0 {
		return strconv.FormatInt(gcsAttrs.Generation, 10)
	}
	return attrs.ETag
}

func (s *BlobStore) DeleteSlug(ctx context.Context, slug string) error {
	keys := []string{CurrentKey(slug)}
	iter := s.bucket.List(&blob.ListOptions{Prefix: "manifests/" + slug + "/"})
	for {
		obj, err := iter.Next(ctx)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		if !obj.IsDir {
			keys = append(keys, obj.Key)
		}
	}
	return s.deleteKeys(ctx, keys)
}

func (s *BlobStore) PruneHistory(ctx context.Context, slug string, keep int) error {
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
	keys := make([]string, 0, len(items)-keep)
	for _, item := range items[keep:] {
		keys = append(keys, ManifestKey(item.Slug, item.ID))
	}
	return s.deleteKeys(ctx, keys)
}

func (s *BlobStore) ListBlobHashes(ctx context.Context) (map[string]struct{}, error) {
	hashes := map[string]struct{}{}
	iter := s.bucket.List(&blob.ListOptions{Prefix: "blobs/sha256/"})
	for {
		obj, err := iter.Next(ctx)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		if obj.IsDir {
			continue
		}
		hash := strings.TrimPrefix(obj.Key, "blobs/sha256/")
		if len(hash) == 64 {
			hashes[hash] = struct{}{}
		}
	}
	return hashes, nil
}

func (s *BlobStore) MoveBlobToTrash(ctx context.Context, hash string) error {
	if err := s.bucket.Copy(ctx, "_trash/"+hash, BlobKey(hash), nil); err != nil {
		if IsNotFound(err) {
			return nil
		}
		return err
	}
	if err := s.bucket.Delete(ctx, BlobKey(hash)); err != nil && !IsNotFound(err) {
		return err
	}
	return nil
}

func (s *BlobStore) DeleteExpiredTrash(ctx context.Context, ttl time.Duration) error {
	cutoff := time.Now().Add(-ttl)
	var keys []string
	iter := s.bucket.List(&blob.ListOptions{Prefix: "_trash/"})
	for {
		obj, err := iter.Next(ctx)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		if !obj.IsDir && obj.ModTime.Before(cutoff) {
			keys = append(keys, obj.Key)
		}
	}
	return s.deleteKeys(ctx, keys)
}

func (s *BlobStore) deleteKeys(ctx context.Context, keys []string) error {
	for _, key := range keys {
		if err := s.bucket.Delete(ctx, key); err != nil && !IsNotFound(err) {
			return err
		}
	}
	return nil
}
