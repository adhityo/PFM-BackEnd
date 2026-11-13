package storage

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"mime"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// S3Config configures the document store.
type S3Config struct {
	Bucket       string
	Region       string
	Endpoint     string // only for S3-compatible stores such as MinIO
	UsePathStyle bool
	KMSKeyID     string
}

// S3DocumentStorage stores documents in a private bucket with server-side encryption.
type S3DocumentStorage struct {
	client  *s3.Client
	presign *s3.PresignClient
	cfg     S3Config
}

// NewS3DocumentStorage loads AWS credentials from the default chain
// (env, shared config, IAM role).
func NewS3DocumentStorage(ctx context.Context, cfg S3Config) (*S3DocumentStorage, error) {
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(cfg.Region))
	if err != nil {
		return nil, fmt.Errorf("load aws config: %w", err)
	}
	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		if cfg.Endpoint != "" {
			o.BaseEndpoint = aws.String(cfg.Endpoint)
		}
		o.UsePathStyle = cfg.UsePathStyle
	})
	return &S3DocumentStorage{client: client, presign: s3.NewPresignClient(client), cfg: cfg}, nil
}

func (s *S3DocumentStorage) Bucket() string { return s.cfg.Bucket }

func (s *S3DocumentStorage) Put(ctx context.Context, key string, r io.Reader, size int64, contentType, sha256Hex string) error {
	sum, err := hex.DecodeString(sha256Hex)
	if err != nil {
		return fmt.Errorf("checksum: %w", err)
	}
	in := &s3.PutObjectInput{
		Bucket:         aws.String(s.cfg.Bucket),
		Key:            aws.String(key),
		Body:           r,
		ContentLength:  aws.Int64(size),
		ContentType:    aws.String(contentType),
		ChecksumSHA256: aws.String(base64.StdEncoding.EncodeToString(sum)),
	}
	if s.cfg.KMSKeyID != "" {
		in.ServerSideEncryption = types.ServerSideEncryptionAwsKms
		in.SSEKMSKeyId = aws.String(s.cfg.KMSKeyID)
	} else {
		in.ServerSideEncryption = types.ServerSideEncryptionAes256
	}
	if _, err := s.client.PutObject(ctx, in); err != nil {
		return fmt.Errorf("s3 put: %w", err)
	}
	return nil
}

func (s *S3DocumentStorage) PresignGet(ctx context.Context, key, downloadName string, ttl time.Duration) (string, error) {
	disposition := mime.FormatMediaType("attachment", map[string]string{"filename": downloadName})
	req, err := s.presign.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket:                     aws.String(s.cfg.Bucket),
		Key:                        aws.String(key),
		ResponseContentDisposition: aws.String(disposition),
	}, s3.WithPresignExpires(ttl))
	if err != nil {
		return "", fmt.Errorf("s3 presign: %w", err)
	}
	return req.URL, nil
}

func (s *S3DocumentStorage) Delete(ctx context.Context, key string) error {
	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(s.cfg.Bucket), Key: aws.String(key)})
	if err != nil {
		return fmt.Errorf("s3 delete: %w", err)
	}
	return nil
}

func (s *S3DocumentStorage) Ping(ctx context.Context) error {
	_, err := s.client.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(s.cfg.Bucket)})
	return err
}
