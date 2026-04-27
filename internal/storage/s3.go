package storage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"

	"github.com/cloudscript-technology/dumpscript/internal/config"
)

func init() {
	Register(config.BackendS3, func(ctx context.Context, cfg *config.Config, log *slog.Logger, opts Options) (Storage, error) {
		var s3opts []S3Option
		if opts.AWSCredentials != nil {
			s3opts = append(s3opts, WithCredentialsProvider(opts.AWSCredentials))
		}
		return NewS3(ctx, cfg, log, s3opts...)
	})
}

// S3 is the Adapter around aws-sdk-go-v2 implementing the Storage port.
// Supports AWS S3, MinIO, and GCS (via S3-compatible HMAC endpoint).
type S3 struct {
	cfg        *config.Config
	log        *slog.Logger
	client     *s3.Client
	uploader   *manager.Uploader
	downloader *manager.Downloader
}

// S3Option is a functional option for NewS3.
type S3Option func(*s3Options)

type s3Options struct {
	credentialsOverride aws.CredentialsProvider
}

// WithCredentialsProvider overrides the credential chain (e.g. IRSA).
func WithCredentialsProvider(p aws.CredentialsProvider) S3Option {
	return func(o *s3Options) { o.credentialsOverride = p }
}

func NewS3(ctx context.Context, cfg *config.Config, log *slog.Logger, opts ...S3Option) (*S3, error) {
	var o s3Options
	for _, opt := range opts {
		opt(&o)
	}

	awsCfg, err := loadAWSConfig(ctx, cfg, o.credentialsOverride)
	if err != nil {
		return nil, err
	}

	isGCS := cfg.S3.EndpointURL != "" && strings.Contains(cfg.S3.EndpointURL, "googleapis.com")
	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		if cfg.S3.EndpointURL != "" {
			o.BaseEndpoint = aws.String(cfg.S3.EndpointURL)
			o.UsePathStyle = !isGCS // GCS prefers virtual-hosted style
		}
	})

	chunk, err := parseSize(cfg.Upload.ChunkSize)
	if err != nil {
		return nil, fmt.Errorf("STORAGE_CHUNK_SIZE: %w", err)
	}

	uploader := manager.NewUploader(client, func(u *manager.Uploader) {
		u.PartSize = chunk
		u.Concurrency = cfg.Upload.Concurrency
	})
	downloader := manager.NewDownloader(client)

	return &S3{
		cfg:        cfg,
		log:        log,
		client:     client,
		uploader:   uploader,
		downloader: downloader,
	}, nil
}

func loadAWSConfig(ctx context.Context, cfg *config.Config, override aws.CredentialsProvider) (aws.Config, error) {
	opts := []func(*awsconfig.LoadOptions) error{}
	if cfg.S3.Region != "" {
		opts = append(opts, awsconfig.WithRegion(cfg.S3.Region))
	}
	switch {
	case override != nil:
		opts = append(opts, awsconfig.WithCredentialsProvider(override))
	case cfg.S3.AccessKeyID != "" && cfg.S3.SecretAccessKey != "":
		opts = append(opts, awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(
				cfg.S3.AccessKeyID, cfg.S3.SecretAccessKey, cfg.S3.SessionToken,
			),
		))
	}
	return awsconfig.LoadDefaultConfig(ctx, opts...)
}

func (s *S3) Upload(ctx context.Context, localPath, key string) error {
	f, err := os.Open(localPath)
	if err != nil {
		return err
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		return fmt.Errorf("stat local: %w", err)
	}
	localSize := fi.Size()

	in := &s3.PutObjectInput{
		Bucket: aws.String(s.cfg.S3.Bucket),
		Key:    aws.String(key),
		Body:   f,
		// Server-side integrity: AWS computes SHA-256 of every part and
		// rejects the upload if any byte was corrupted in transit.
		ChecksumAlgorithm: s3types.ChecksumAlgorithmSha256,
	}
	if s.cfg.S3.StorageClass != "" {
		in.StorageClass = s3types.StorageClass(s.cfg.S3.StorageClass)
	}
	if _, err = s.uploader.Upload(ctx, in); err != nil {
		return fmt.Errorf("s3 upload: %w", err)
	}

	// Post-upload size sanity: catches the case where AWS accepted the parts
	// but the final object ended up truncated (rare but observed under
	// buggy proxies / aborted multipart sessions).
	head, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(s.cfg.S3.Bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return fmt.Errorf("s3 verify head: %w", err)
	}
	if head.ContentLength != nil && *head.ContentLength != localSize {
		return fmt.Errorf("s3 upload integrity check failed: local=%d remote=%d (key=%s)",
			localSize, *head.ContentLength, key)
	}
	s.log.Debug("s3 upload integrity verified",
		"key", key, "size", localSize, "checksum_alg", "SHA256")
	return nil
}

func (s *S3) UploadBytes(ctx context.Context, data []byte, key string) error {
	in := &s3.PutObjectInput{
		Bucket: aws.String(s.cfg.S3.Bucket),
		Key:    aws.String(key),
		Body:   bytes.NewReader(data),
	}
	if s.cfg.S3.StorageClass != "" {
		in.StorageClass = s3types.StorageClass(s.cfg.S3.StorageClass)
	}
	_, err := s.client.PutObject(ctx, in)
	return err
}

func (s *S3) Exists(ctx context.Context, key string) (bool, error) {
	_, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(s.cfg.S3.Bucket),
		Key:    aws.String(key),
	})
	if err == nil {
		return true, nil
	}
	var notFound *s3types.NotFound
	if errors.As(err, &notFound) {
		return false, nil
	}
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		code := apiErr.ErrorCode()
		if code == "NotFound" || code == "NoSuchKey" || code == "404" {
			return false, nil
		}
	}
	return false, err
}

func (s *S3) Download(ctx context.Context, key, localPath string) error {
	f, err := os.Create(localPath)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = s.downloader.Download(ctx, f, &s3.GetObjectInput{
		Bucket: aws.String(s.cfg.S3.Bucket),
		Key:    aws.String(key),
	})
	return err
}

func (s *S3) List(ctx context.Context, prefix string) ([]Object, error) {
	var out []Object
	p := s3.NewListObjectsV2Paginator(s.client, &s3.ListObjectsV2Input{
		Bucket: aws.String(s.cfg.S3.Bucket),
		Prefix: aws.String(prefix),
	})
	for p.HasMorePages() {
		page, err := p.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, obj := range page.Contents {
			out = append(out, Object{
				Key:      aws.ToString(obj.Key),
				Size:     aws.ToInt64(obj.Size),
				Modified: aws.ToTime(obj.LastModified),
			})
		}
	}
	return out, nil
}

func (s *S3) Delete(ctx context.Context, key string) error {
	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.cfg.S3.Bucket),
		Key:    aws.String(key),
	})
	return err
}

func (s *S3) DisplayPath(key string) string {
	u := url.URL{Scheme: "s3", Host: s.cfg.S3.Bucket, Path: "/" + key}
	return u.String()
}
