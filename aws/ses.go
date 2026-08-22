package aws

import (
	"context"
	"errors"
	"mime"
	"strconv"
	"strings"

	"github.com/aws/aws-sdk-go-v2/service/sesv2"
	"github.com/aws/aws-sdk-go-v2/service/sesv2/types"
	"github.com/hatami57/microjet/core/errorx"
)

// SESSendEmailRequest is one email sent through SESSendEmail. Only the
// recipients, the subject and one of the bodies are required; the sender falls
// back to the [aws] sesSenderEmail/sesSenderName configuration.
type SESSendEmailRequest struct {
	// From is the sender address, either bare ("no-reply@example.com") or a full
	// RFC 5322 address ("Support" <no-reply@example.com>). Empty means the
	// configured default sender. The address (or its domain) must be a verified
	// SES identity.
	From string
	// FromName is the display name shown to the recipient. It applies only to a
	// bare From address, and overrides the configured default sender name.
	FromName string

	// To recipients all appear in the same message and see each other; send one
	// email per person where that matters. Blank entries are dropped, so a list
	// assembled from optional fields needs no cleanup.
	To      []string
	CC      []string
	BCC     []string
	ReplyTo []string

	Subject  string
	HTMLBody string
	TextBody string

	// ConfigurationSetName overrides, for this message, the configured
	// [aws] sesConfigurationSet. A configuration set is what publishes delivery,
	// bounce and complaint events to SNS/EventBridge.
	ConfigurationSetName string
	// Tags are SES message tags, attached to the events the configuration set
	// publishes; use them to correlate an event with your own message ID. Names
	// and values are limited to letters, digits, dashes and underscores.
	Tags map[string]string
}

// SESSendEmail sends req as a simple (non-raw, non-templated) email and returns
// the message ID SES assigned to it — the ID that later delivery, bounce and
// complaint events carry, worth storing alongside the message.
//
// A returned error is an internal error wrapping the SDK failure, except for a
// malformed request (no recipient, no subject, no body), which is a bad-request
// error. Use SESIsPermanentFailure to decide whether a failed send is worth
// retrying.
func (a *AWS) SESSendEmail(ctx context.Context, req *SESSendEmailRequest) (string, error) {
	if req == nil {
		return "", errorx.NewInternalError("aws", "req is nil")
	}
	if a.SESClient == nil {
		return "", errorx.NewInternalError("aws", "ses client is not configured")
	}

	from, err := a.resolveSESSender(req)
	if err != nil {
		return "", err
	}

	to, cc, bcc := cleanAddresses(req.To), cleanAddresses(req.CC), cleanAddresses(req.BCC)
	recipients := len(to) + len(cc) + len(bcc)
	if recipients == 0 {
		return "", errorx.NewBadRequestError("aws", "ses email has no recipient")
	}
	if strings.TrimSpace(req.Subject) == "" {
		return "", errorx.NewBadRequestError("aws", "ses email subject is empty")
	}
	if req.HTMLBody == "" && req.TextBody == "" {
		return "", errorx.NewBadRequestError("aws", "ses email body is empty")
	}

	body := &types.Body{}
	if req.HTMLBody != "" {
		body.Html = utf8Content(req.HTMLBody)
	}
	if req.TextBody != "" {
		body.Text = utf8Content(req.TextBody)
	}

	input := &sesv2.SendEmailInput{
		FromEmailAddress: &from,
		Destination: &types.Destination{
			ToAddresses:  to,
			CcAddresses:  cc,
			BccAddresses: bcc,
		},
		ReplyToAddresses: cleanAddresses(req.ReplyTo),
		Content: &types.EmailContent{
			Simple: &types.Message{
				Subject: utf8Content(req.Subject),
				Body:    body,
			},
		},
	}
	if cs := a.resolveSESConfigurationSet(req); cs != "" {
		input.ConfigurationSetName = &cs
	}
	for name, value := range req.Tags {
		input.EmailTags = append(input.EmailTags, types.MessageTag{Name: &name, Value: &value})
	}

	output, err := a.SESClient.SendEmail(ctx, input)
	if err != nil {
		// Recipient addresses stay out of the error: it travels into logs, and the
		// count is enough to tell one failure from another.
		return "", errorx.NewInternalError("aws", "ses send email failed",
			"from", from, "recipients", recipients).WithInner(err)
	}

	messageID := ""
	if output.MessageId != nil {
		messageID = *output.MessageId
	}
	a.Logger.Info("SES send email successfully", "messageID", messageID, "recipients", recipients)
	return messageID, nil
}

// SESIsPermanentFailure reports whether err is a failure that sending the same
// message again cannot fix — a rejected message, an unverified sender domain, a
// malformed request, a suspended account. A retry loop (a delivery sweeper, say)
// should mark such a send failed for good instead of scheduling another attempt.
// Everything else — throttling, service errors, transport failures — is treated
// as transient and reported as retryable.
func SESIsPermanentFailure(err error) bool {
	if err == nil {
		return false
	}
	if errorType, ok := errorx.GetErrorType(err); ok && errorType == errorx.BadRequestErrorType {
		return true
	}

	var (
		accountSuspended    *types.AccountSuspendedException
		badRequest          *types.BadRequestException
		mailFromNotVerified *types.MailFromDomainNotVerifiedException
		notFound            *types.NotFoundException
		rejected            *types.MessageRejected
		sendingPaused       *types.SendingPausedException
	)
	return errors.As(err, &accountSuspended) ||
		errors.As(err, &badRequest) ||
		errors.As(err, &mailFromNotVerified) ||
		errors.As(err, &notFound) ||
		errors.As(err, &rejected) ||
		errors.As(err, &sendingPaused)
}

// SESFormatAddress renders name and email as an RFC 5322 address
// (`"Support" <no-reply@example.com>`), MIME-encoding a non-ASCII display name
// so mail clients show it correctly. An empty name, or an email that already
// carries one, is returned unchanged.
func SESFormatAddress(name, email string) string {
	name, email = strings.TrimSpace(name), strings.TrimSpace(email)
	if name == "" || strings.ContainsAny(email, "<>") {
		return email
	}
	// Encode returns the input untouched when it is plain ASCII; such a name still
	// needs quoting, because commas and colons are RFC 5322 specials. An
	// encoded-word, in contrast, must not be quoted.
	if encoded := mime.QEncoding.Encode("utf-8", name); encoded != name {
		return encoded + " <" + email + ">"
	}
	return strconv.Quote(name) + " <" + email + ">"
}

func (a *AWS) resolveSESSender(req *SESSendEmailRequest) (string, error) {
	email, name := strings.TrimSpace(req.From), strings.TrimSpace(req.FromName)
	if email == "" {
		email = strings.TrimSpace(a.DefaultSES.SenderEmail)
		if email == "" {
			return "", errorx.NewInternalError("aws", "default ses sender email is not configured")
		}
		if name == "" {
			name = a.DefaultSES.SenderName
		}
	}
	return SESFormatAddress(name, email), nil
}

func (a *AWS) resolveSESConfigurationSet(req *SESSendEmailRequest) string {
	if cs := strings.TrimSpace(req.ConfigurationSetName); cs != "" {
		return cs
	}
	return strings.TrimSpace(a.DefaultSES.ConfigurationSet)
}

// cleanAddresses trims the given addresses and drops the empty ones, so a caller
// assembling a recipient list from optional fields does not have to.
func cleanAddresses(addresses []string) []string {
	var cleaned []string
	for _, address := range addresses {
		if trimmed := strings.TrimSpace(address); trimmed != "" {
			cleaned = append(cleaned, trimmed)
		}
	}
	return cleaned
}

func utf8Content(data string) *types.Content {
	charset := "UTF-8"
	return &types.Content{Data: &data, Charset: &charset}
}
