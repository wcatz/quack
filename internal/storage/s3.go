package storage

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type S3Client struct {
	client *s3.Client
	bucket string
	logger *slog.Logger
}

func NewS3Client(endpoint, region, accessKey, secretKey, bucket string, logger *slog.Logger) (*S3Client, error) {
	cfg := aws.Config{
		Region:      region,
		Credentials: credentials.NewStaticCredentialsProvider(accessKey, secretKey, ""),
	}

	client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		if endpoint != "" {
			o.BaseEndpoint = aws.String(endpoint)
		}
		o.UsePathStyle = true
	})

	return &S3Client{
		client: client,
		bucket: bucket,
		logger: logger,
	}, nil
}

// EnsureBucket creates the bucket if it doesn't exist.
func (s *S3Client) EnsureBucket(ctx context.Context) error {
	_, err := s.client.HeadBucket(ctx, &s3.HeadBucketInput{
		Bucket: aws.String(s.bucket),
	})
	if err == nil {
		return nil
	}

	s.logger.Info("creating bucket", "bucket", s.bucket)
	_, err = s.client.CreateBucket(ctx, &s3.CreateBucketInput{
		Bucket: aws.String(s.bucket),
	})
	if err != nil {
		return fmt.Errorf("create bucket %s: %w", s.bucket, err)
	}
	return nil
}

// Put uploads data to S3 with the given key and metadata.
func (s *S3Client) Put(ctx context.Context, key string, data []byte, contentType string, metadata map[string]string) error {
	_, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:       aws.String(s.bucket),
		Key:          aws.String(key),
		Body:         bytes.NewReader(data),
		ContentType:  aws.String(contentType),
		CacheControl: aws.String("public, max-age=31536000, immutable"),
		Metadata:     metadata,
	})
	if err != nil {
		return fmt.Errorf("s3 put %s: %w", key, err)
	}
	return nil
}

// Exists checks if an object exists in the bucket.
func (s *S3Client) Exists(ctx context.Context, key string) (bool, error) {
	_, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		// HeadObject returns 404 as a smithy error; check the error string
		// since the concrete error types vary across SDK versions.
		if strings.Contains(err.Error(), "NotFound") ||
			strings.Contains(err.Error(), "404") ||
			strings.Contains(err.Error(), "NoSuchKey") {
			return false, nil
		}
		return false, fmt.Errorf("s3 head %s: %w", key, err)
	}
	return true, nil
}

// PresignedURL generates a pre-signed GET URL.
func (s *S3Client) PresignedURL(ctx context.Context, key string, expiry time.Duration) (string, error) {
	presignClient := s3.NewPresignClient(s.client)
	req, err := presignClient.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	}, func(opts *s3.PresignOptions) {
		opts.Expires = expiry
	})
	if err != nil {
		return "", fmt.Errorf("s3 presign %s: %w", key, err)
	}
	return req.URL, nil
}

// ListKeys returns all object keys under the given prefix.
func (s *S3Client) ListKeys(ctx context.Context, prefix string) ([]string, error) {
	var keys []string
	paginator := s3.NewListObjectsV2Paginator(s.client, &s3.ListObjectsV2Input{
		Bucket: aws.String(s.bucket),
		Prefix: aws.String(prefix),
	})

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("s3 list %s: %w", prefix, err)
		}
		for _, obj := range page.Contents {
			keys = append(keys, aws.ToString(obj.Key))
		}
	}
	return keys, nil
}

// ContentHash returns the SHA-256 hash (first 12 hex chars) of the given data.
func ContentHash(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])[:12]
}

// ObjectKey builds the S3 object key from content hash and extension.
func ObjectKey(hash, ext string) string {
	if ext == ".gif" {
		return fmt.Sprintf("gifs/%s%s", hash, ext)
	}
	return fmt.Sprintf("images/%s%s", hash, ext)
}

// Delete removes an object from S3.
func (s *S3Client) Delete(ctx context.Context, key string) error {
	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return fmt.Errorf("s3 delete %s: %w", key, err)
	}
	return nil
}

// GetPublicURL returns a direct URL (if publicURL is configured) or a presigned URL.
func (s *S3Client) GetPublicURL(ctx context.Context, key, publicBaseURL string, expiry time.Duration) (string, error) {
	if publicBaseURL != "" {
		return fmt.Sprintf("%s/%s/%s", strings.TrimRight(publicBaseURL, "/"), s.bucket, key), nil
	}
	return s.PresignedURL(ctx, key, expiry)
}

// ReadAll returns the body of an S3 object.
func (s *S3Client) ReadAll(ctx context.Context, key string) ([]byte, error) {
	result, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, fmt.Errorf("s3 get %s: %w", key, err)
	}
	defer result.Body.Close()
	return io.ReadAll(result.Body)
}
