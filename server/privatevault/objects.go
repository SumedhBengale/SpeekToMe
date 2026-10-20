package privatevault

import (
	"context"
	"fmt"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
	"io"
	"net/http"
	"time"
)

type Objects interface {
	Put(context.Context, string, io.Reader) error
	ReadAt(context.Context, string, []byte, int64) (int, error)
	Delete(context.Context, string) error
}
type R2 struct {
	client *s3.S3
	bucket string
}

func NewR2(account, bucket, access, secret string) (*R2, error) {
	if account == "" || bucket == "" || access == "" || secret == "" {
		return nil, fmt.Errorf("R2 configuration incomplete")
	}
	sess, e := session.NewSession(&aws.Config{Region: aws.String("auto"), Endpoint: aws.String("https://" + account + ".r2.cloudflarestorage.com"), S3ForcePathStyle: aws.Bool(true), Credentials: credentials.NewStaticCredentials(access, secret, ""), HTTPClient: &http.Client{Timeout: 20 * time.Minute}, MaxRetries: aws.Int(2)})
	if e != nil {
		return nil, ErrUnavailable
	}
	return &R2{s3.New(sess), bucket}, nil
}
func (r *R2) Put(ctx context.Context, key string, body io.Reader) error {
	uploader := s3manager.NewUploaderWithClient(r.client, func(u *s3manager.Uploader) { u.PartSize = 8 << 20; u.Concurrency = 1; u.LeavePartsOnError = false })
	_, e := uploader.UploadWithContext(ctx, &s3manager.UploadInput{Bucket: aws.String(r.bucket), Key: aws.String(key), Body: body, ContentType: aws.String("application/octet-stream"), CacheControl: aws.String("no-store")})
	if e != nil {
		return ErrUnavailable
	}
	return nil
}
func (r *R2) ReadAt(ctx context.Context, key string, p []byte, off int64) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if off < 0 {
		return 0, io.ErrUnexpectedEOF
	}
	out, e := r.client.GetObjectWithContext(ctx, &s3.GetObjectInput{Bucket: aws.String(r.bucket), Key: aws.String(key), Range: aws.String(fmt.Sprintf("bytes=%d-%d", off, off+int64(len(p))-1))})
	if e != nil {
		return 0, ErrUnavailable
	}
	defer out.Body.Close()
	n, e := io.ReadFull(out.Body, p)
	if e == io.ErrUnexpectedEOF {
		e = io.EOF
	}
	return n, e
}
func (r *R2) Delete(ctx context.Context, key string) error {
	_, e := r.client.DeleteObjectWithContext(ctx, &s3.DeleteObjectInput{Bucket: aws.String(r.bucket), Key: aws.String(key)})
	if e != nil {
		return ErrUnavailable
	}
	return nil
}
