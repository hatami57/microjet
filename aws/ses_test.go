package aws

import (
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/sesv2"
	"github.com/aws/aws-sdk-go-v2/service/sesv2/types"
	"github.com/hatami57/microjet/core/errorx"
)

func TestSESFormatAddress(t *testing.T) {
	tests := []struct {
		name     string
		display  string
		email    string
		expected string
	}{
		{"no display name", "", "no-reply@example.com", "no-reply@example.com"},
		{"ascii name is quoted", "Support", "no-reply@example.com", `"Support" <no-reply@example.com>`},
		{"special characters stay inside the quotes", "Acme, Inc.", "a@b.com", `"Acme, Inc." <a@b.com>`},
		{"embedded quote is escaped", `He said "hi"`, "a@b.com", `"He said \"hi\"" <a@b.com>`},
		{"non-ascii name is mime encoded", "Müller", "a@b.com", "=?utf-8?q?M=C3=BCller?= <a@b.com>"},
		{"address that already has a name is left alone", "Support", "Sales <a@b.com>", "Sales <a@b.com>"},
		{"inputs are trimmed", "  Support  ", "  a@b.com  ", `"Support" <a@b.com>`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := SESFormatAddress(tc.display, tc.email); got != tc.expected {
				t.Errorf("SESFormatAddress(%q, %q) = %q, want %q", tc.display, tc.email, got, tc.expected)
			}
		})
	}
}

func TestResolveSESSenderFallsBackToConfig(t *testing.T) {
	client := &AWS{DefaultSES: SESConfig{SenderEmail: "no-reply@example.com", SenderName: "Support"}}

	got, err := client.resolveSESSender(&SESSendEmailRequest{})
	if err != nil {
		t.Fatalf("resolveSESSender: %v", err)
	}
	if want := `"Support" <no-reply@example.com>`; got != want {
		t.Errorf("sender = %q, want %q", got, want)
	}
}

func TestResolveSESSenderRequestWins(t *testing.T) {
	client := &AWS{DefaultSES: SESConfig{SenderEmail: "no-reply@example.com", SenderName: "Support"}}

	// A request address drops the configured display name; it belongs to the
	// configured address, not to this one.
	got, err := client.resolveSESSender(&SESSendEmailRequest{From: "billing@example.com"})
	if err != nil {
		t.Fatalf("resolveSESSender: %v", err)
	}
	if want := "billing@example.com"; got != want {
		t.Errorf("sender = %q, want %q", got, want)
	}

	got, err = client.resolveSESSender(&SESSendEmailRequest{FromName: "Billing"})
	if err != nil {
		t.Fatalf("resolveSESSender: %v", err)
	}
	if want := `"Billing" <no-reply@example.com>`; got != want {
		t.Errorf("sender = %q, want %q", got, want)
	}
}

func TestResolveSESSenderWithoutConfiguredSender(t *testing.T) {
	client := &AWS{}

	if _, err := client.resolveSESSender(&SESSendEmailRequest{}); err == nil {
		t.Fatal("expected an error when neither the request nor the config names a sender")
	}
}

func TestResolveSESConfigurationSet(t *testing.T) {
	client := &AWS{DefaultSES: SESConfig{ConfigurationSet: "default-set"}}

	if got := client.resolveSESConfigurationSet(&SESSendEmailRequest{}); got != "default-set" {
		t.Errorf("configuration set = %q, want the configured default", got)
	}
	if got := client.resolveSESConfigurationSet(&SESSendEmailRequest{ConfigurationSetName: "per-message"}); got != "per-message" {
		t.Errorf("configuration set = %q, want the per-request override", got)
	}
	if got := (&AWS{}).resolveSESConfigurationSet(&SESSendEmailRequest{}); got != "" {
		t.Errorf("configuration set = %q, want empty when none is configured", got)
	}
}

func TestSESSendEmailRejectsMalformedRequests(t *testing.T) {
	// The client is present but unusable (no credentials, no endpoint): every case
	// here must be rejected by validation before any call goes out.
	client := &AWS{
		SESClient:  sesv2.New(sesv2.Options{}),
		DefaultSES: SESConfig{SenderEmail: "no-reply@example.com"},
	}
	valid := SESSendEmailRequest{To: []string{"user@example.com"}, Subject: "Hi", TextBody: "Body"}

	tests := []struct {
		name    string
		mutate  func(*SESSendEmailRequest)
		wantErr string
	}{
		{"no recipient", func(r *SESSendEmailRequest) { r.To = nil }, "no recipient"},
		{"blank recipients only", func(r *SESSendEmailRequest) { r.To = []string{"", "  "} }, "no recipient"},
		{"no subject", func(r *SESSendEmailRequest) { r.Subject = "  " }, "subject is empty"},
		{"no body", func(r *SESSendEmailRequest) { r.TextBody = "" }, "body is empty"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := valid
			tc.mutate(&req)

			_, err := client.SESSendEmail(t.Context(), &req)
			if err == nil {
				t.Fatalf("expected an error for %s", tc.name)
			}
			if errorType, ok := errorx.GetErrorType(err); !ok || errorType != errorx.BadRequestErrorType {
				t.Errorf("error type = %v, want a bad-request error: %v", errorType, err)
			}
			if !SESIsPermanentFailure(err) {
				t.Error("a malformed request must count as a permanent failure")
			}
		})
	}
}

func TestSESIsPermanentFailure(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{"nil", nil, false},
		{"rejected message", &types.MessageRejected{}, true},
		{"unverified mail-from domain", &types.MailFromDomainNotVerifiedException{}, true},
		{"bad request", &types.BadRequestException{}, true},
		{"suspended account", &types.AccountSuspendedException{}, true},
		{"throttled", &types.TooManyRequestsException{}, false},
		{"service error", &types.InternalServiceErrorException{}, false},
		{"transport error", errors.New("connection reset by peer"), false},
		{
			"wrapped in an internal error, as SESSendEmail returns it",
			errorx.NewInternalError("aws", "ses send email failed").WithInner(&types.MessageRejected{}),
			true,
		},
		{
			"internal error alone stays retryable",
			errorx.NewInternalError("aws", "ses send email failed").WithInner(errors.New("timeout")),
			false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := SESIsPermanentFailure(tc.err); got != tc.expected {
				t.Errorf("SESIsPermanentFailure(%v) = %v, want %v", tc.err, got, tc.expected)
			}
		})
	}
}
