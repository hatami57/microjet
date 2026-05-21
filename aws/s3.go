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

func (a *AWS) S3DownloadFiles(reqs []S3DownloadFileRequest, maxWorkers int) error {
	if maxWorkers <= 0 {
		maxWorkers = 1
	}
	if a.S3Client == nil {
		return fmt.Errorf("s3 client is not configured")
	}

	sem := make(chan struct{}, maxWorkers)
	errCh := make(chan error, len(reqs))

	for _, req := range reqs {
		go func(r S3DownloadFileRequest) {
			sem <- struct{}{}
			defer func() { <-sem }()
			errCh <- a.internalS3DownloadFile(&r)
		}(req)
	}

	for range reqs {
		if err := <-errCh; err != nil {
			return err
		}
	}
	return nil
}

func (a *AWS) S3DownloadFile(req *S3DownloadFileRequest) error {
	if a.S3Client == nil {
		return fmt.Errorf("s3 client is not configured")
	}
	return a.internalS3DownloadFile(req)
}

func (a *AWS) internalS3DownloadFile(req *S3DownloadFileRequest) error {
	f, err := os.Create(req.LocalFilePath)
	if err != nil {
		return fmt.Errorf("failed to create file: %v", err)
	}
	defer f.Close()

	a.Logger.Debug("downloading from s3", slog.String("path", req.LocalFilePath))
	result, err := a.S3Client.GetObject(context.TODO(), &s3.GetObjectInput{
		Bucket: aws.String(req.BucketName),
		Key:    aws.String(req.ObjectKey),
	})
	if err != nil {
		return fmt.Errorf("failed to get object from s3: %v", err)
	}
	defer result.Body.Close()

	buffer := make([]byte, 20*1024*1024)
	for {
		n, err := result.Body.Read(buffer)
		if err != nil && err != io.EOF {
			return fmt.Errorf("failed to read from s3: %v", err)
		}
		if n == 0 {
			break
		}
		if _, err := f.Write(buffer[:n]); err != nil {
			return fmt.Errorf("failed to write to file: %v", err)
		}
	}
	a.Logger.Debug("download completed from s3", slog.String("path", req.LocalFilePath))
	return nil
}

func (a *AWS) S3UploadFile(req *S3UploadFileRequest) error {
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

	uploader := manager.NewUploader(a.S3Client)

	var tagging *string
	if req.Headers != nil {
		values := url.Values{}
		for key, value := range req.Headers {
			values.Add(key, value)
		}
		tagging = aws.String(values.Encode())
	}

	a.Logger.Debug("uploading to s3", slog.String("path", req.LocalFilePath))
	_, err = uploader.Upload(context.TODO(), &s3.PutObjectInput{
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
