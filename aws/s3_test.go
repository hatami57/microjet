package aws

import (
	"context"
	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestS3DownloadReplacesOnlyCompleteFiles(t *testing.T) {
	for _, mode := range []string{"success", "missing", "truncated"} {
		t.Run(mode, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch mode {
				case "missing":
					w.WriteHeader(404)
				case "truncated":
					w.Header().Set("Content-Length", "100")
					_, _ = w.Write([]byte("partial"))
				default:
					_, _ = w.Write([]byte("complete"))
				}
			}))
			defer srv.Close()
			client := s3.NewFromConfig(awssdk.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("key", "secret", "")}, func(o *s3.Options) { o.BaseEndpoint = &srv.URL; o.UsePathStyle = true; o.RetryMaxAttempts = 1 })
			a := &AWS{S3Client: client, Logger: slog.Default()}
			dir := t.TempDir()
			path := filepath.Join(dir, "download")
			if err := os.WriteFile(path, []byte("original"), 0600); err != nil {
				t.Fatal(err)
			}
			err := a.S3DownloadFile(context.Background(), &S3DownloadFileRequest{BucketName: "bucket", ObjectKey: "object", LocalFilePath: path})
			if (err == nil) != (mode == "success") {
				t.Fatalf("error = %v", err)
			}
			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			want := "original"
			if mode == "success" {
				want = "complete"
			}
			if string(got) != want {
				t.Fatalf("contents = %q, want %q", got, want)
			}
			files, err := os.ReadDir(dir)
			if err != nil {
				t.Fatal(err)
			}
			if len(files) != 1 {
				t.Fatalf("temporary files remain: %v", files)
			}
			if err := a.S3DownloadFile(context.Background(), nil); err == nil {
				t.Fatal("accepted nil request")
			}
		})
	}
}
