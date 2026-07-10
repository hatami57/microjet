package aws

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"golang.org/x/sync/errgroup"
)

type S3DownloadFileRequest struct {
	BucketName    string
	ObjectKey     string
	LocalFilePath string
}

type S3UploadFileRequest struct {
	BucketName    string
	ObjectKey     string
	ContentType   string
	Headers       map[string]string
	LocalFilePath string
}

func (a *AWS) S3DownloadFiles(ctx context.Context, reqs []S3DownloadFileRequest, maxWorkers int) error {
	if maxWorkers <= 0 {
		maxWorkers = 1
	}
	if a.S3Client == nil {
		return fmt.Errorf("s3 client is not configured")
	}

	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(maxWorkers)
	for _, req := range reqs {
		g.Go(func() error {
			// errgroup cancels ctx on first error; honor it before each download
			// so we don't keep launching work after a failure.
			if err := ctx.Err(); err != nil {
				return err
			}
			return a.internalS3DownloadFile(ctx, &req)
		})
	}
	return g.Wait()
}

func (a *AWS) S3DownloadFile(ctx context.Context, req *S3DownloadFileRequest) error {
	if a.S3Client == nil {
		return fmt.Errorf("s3 client is not configured")
	}
	return a.internalS3DownloadFile(ctx, req)
}

func (a *AWS) internalS3DownloadFile(ctx context.Context, req *S3DownloadFileRequest) error {
	f, err := os.Create(req.LocalFilePath)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer f.Close()

	a.Logger.Debug("downloading from s3", slog.String("path", req.LocalFilePath))
	result, err := a.S3Client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(req.BucketName),
		Key:    aws.String(req.ObjectKey),
	})
	if err != nil {
		return fmt.Errorf("failed to get object from s3: %w", err)
	}
	defer result.Body.Close()

	if _, err := io.Copy(f, result.Body); err != nil {
		return fmt.Errorf("failed to stream object to file: %w", err)
	}
	a.Logger.Debug("download completed from s3", slog.String("path", req.LocalFilePath))
	return nil
}

func (a *AWS) S3UploadFile(ctx context.Context, req *S3UploadFileRequest) error {
	if req == nil {
		return fmt.Errorf("req is nil")
	}
	if a.S3Client == nil {
		return fmt.Errorf("s3 client is not configured")
	}

	file, err := os.Open(req.LocalFilePath)
	if err != nil {
		return err
	}
	defer file.Close()

	// TODO: migrate to feature/s3/transfermanager once it stabilizes.
	//lint:ignore SA1019 manager.Uploader is the current stable multipart uploader; its successor lives in a separate module.
	uploader := manager.NewUploader(a.S3Client) //nolint:staticcheck // SA1019: see note above (golangci-lint ignores //lint:ignore).

	var tagging *string
	if req.Headers != nil {
		values := url.Values{}
		for key, value := range req.Headers {
			values.Add(key, value)
		}
		tagging = aws.String(values.Encode())
	}

	a.Logger.Debug("uploading to s3", slog.String("path", req.LocalFilePath))
	//lint:ignore SA1019 see note above; using the stable manager.Uploader API.
	_, err = uploader.Upload(ctx, &s3.PutObjectInput{ //nolint:staticcheck // SA1019: see note above.
		Bucket:      &req.BucketName,
		Key:         &req.ObjectKey,
		ContentType: &req.ContentType,
		Body:        file,
		Tagging:     tagging,
	})
	if err != nil {
		return err
	}
	a.Logger.Debug("upload completed from s3", slog.String("path", req.LocalFilePath))
	return nil
}
