package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"mime"
	"strings"

	"golang.org/x/oauth2"
	"google.golang.org/api/gmail/v1"
	"google.golang.org/api/option"
)

// Attachment represents a file attachment for sending emails.
type Attachment struct {
	Filename string `json:"filename"`
	MimeType string `json:"mime_type"`
	Data     string `json:"data"` // base64-encoded file content
}

// GmailService wraps the Google Gmail API.
type GmailService struct {
	svc *gmail.Service
}

// NewGmailService creates a Gmail API client from a token source.
func NewGmailService(ctx context.Context, ts oauth2.TokenSource) (*GmailService, error) {
	svc, err := gmail.NewService(ctx, option.WithTokenSource(ts))
	if err != nil {
		return nil, fmt.Errorf("create gmail service: %w", err)
	}
	return &GmailService{svc: svc}, nil
}

// JSON output types

type emailJSON struct {
	ID          string           `json:"id"`
	ThreadID    string           `json:"threadId"`
	Subject     string           `json:"subject"`
	From        string           `json:"from"`
	To          string           `json:"to"`
	Cc          string           `json:"cc,omitempty"`
	Date        string           `json:"date"`
	Snippet     string           `json:"snippet,omitempty"`
	Body        string           `json:"body,omitempty"`
	Labels      []string         `json:"labels,omitempty"`
	Attachments []attachmentJSON `json:"attachments,omitempty"`
}

type attachmentJSON struct {
	ID       string `json:"id"`
	Filename string `json:"filename"`
	MimeType string `json:"mimeType"`
	Size     int64  `json:"size"`
}

type labelJSON struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Type           string `json:"type,omitempty"`
	MessagesTotal  int64  `json:"messagesTotal,omitempty"`
	MessagesUnread int64  `json:"messagesUnread,omitempty"`
}

// Helper functions

func getHeader(headers []*gmail.MessagePartHeader, name string) string {
	for _, h := range headers {
		if strings.EqualFold(h.Name, name) {
			return h.Value
		}
	}
	return ""
}

func extractEmailBody(part *gmail.MessagePart) (text, htmlBody string) {
	if part == nil {
		return "", ""
	}

	switch {
	case part.MimeType == "text/plain" && part.Body != nil && part.Body.Data != "":
		decoded, err := base64.RawURLEncoding.DecodeString(part.Body.Data)
		if err == nil {
			return string(decoded), ""
		}
	case part.MimeType == "text/html" && part.Body != nil && part.Body.Data != "":
		decoded, err := base64.RawURLEncoding.DecodeString(part.Body.Data)
		if err == nil {
			return "", string(decoded)
		}
	case strings.HasPrefix(part.MimeType, "multipart/"):
		var textResult, htmlResult string
		for _, p := range part.Parts {
			t, h := extractEmailBody(p)
			if t != "" && textResult == "" {
				textResult = t
			}
			if h != "" && htmlResult == "" {
				htmlResult = h
			}
		}
		return textResult, htmlResult
	}
	return "", ""
}

func extractAttachments(part *gmail.MessagePart) []attachmentJSON {
	if part == nil {
		return nil
	}
	var attachments []attachmentJSON
	if part.Filename != "" && part.Body != nil {
		attachments = append(attachments, attachmentJSON{
			ID:       part.Body.AttachmentId,
			Filename: part.Filename,
			MimeType: part.MimeType,
			Size:     part.Body.Size,
		})
	}
	for _, p := range part.Parts {
		attachments = append(attachments, extractAttachments(p)...)
	}
	return attachments
}

func convertMessage(msg *gmail.Message) emailJSON {
	email := emailJSON{
		ID:       msg.Id,
		ThreadID: msg.ThreadId,
		Snippet:  msg.Snippet,
		Labels:   msg.LabelIds,
	}
	if msg.Payload != nil {
		email.Subject = getHeader(msg.Payload.Headers, "Subject")
		email.From = getHeader(msg.Payload.Headers, "From")
		email.To = getHeader(msg.Payload.Headers, "To")
		email.Cc = getHeader(msg.Payload.Headers, "Cc")
		email.Date = getHeader(msg.Payload.Headers, "Date")

		text, htmlBody := extractEmailBody(msg.Payload)
		if text != "" {
			email.Body = text
		} else if htmlBody != "" {
			email.Body = htmlBody
		}

		email.Attachments = extractAttachments(msg.Payload)
	}
	return email
}

// validateAttachments checks that attachment fields are valid for MIME construction.
func validateAttachments(attachments []Attachment) error {
	for i, att := range attachments {
		if att.Filename == "" {
			return fmt.Errorf("attachment[%d]: filename is required", i)
		}
		if att.MimeType == "" {
			return fmt.Errorf("attachment[%d]: mime_type is required", i)
		}
		if strings.ContainsAny(att.MimeType, "\r\n") {
			return fmt.Errorf("attachment[%d]: mime_type contains invalid characters", i)
		}
		if !strings.Contains(att.MimeType, "/") {
			return fmt.Errorf("attachment[%d]: mime_type must be in type/subtype format (e.g. application/pdf)", i)
		}
		if att.Data == "" {
			return fmt.Errorf("attachment[%d]: data is required", i)
		}
	}
	return nil
}

func buildRawEmail(to, subject, body, cc, bcc, inReplyTo string, attachments []Attachment) string {
	var buf strings.Builder

	// Common headers
	buf.WriteString(fmt.Sprintf("To: %s\r\n", to))
	if cc != "" {
		buf.WriteString(fmt.Sprintf("Cc: %s\r\n", cc))
	}
	if bcc != "" {
		buf.WriteString(fmt.Sprintf("Bcc: %s\r\n", bcc))
	}
	buf.WriteString(fmt.Sprintf("Subject: %s\r\n", mime.QEncoding.Encode("utf-8", subject)))
	if inReplyTo != "" {
		buf.WriteString(fmt.Sprintf("In-Reply-To: %s\r\n", inReplyTo))
		buf.WriteString(fmt.Sprintf("References: %s\r\n", inReplyTo))
	}

	if len(attachments) == 0 {
		// Simple plain text email
		buf.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
		buf.WriteString("\r\n")
		buf.WriteString(body)
		return base64.RawURLEncoding.EncodeToString([]byte(buf.String()))
	}

	// MIME multipart email with attachments
	boundary := generateBoundary()
	buf.WriteString("MIME-Version: 1.0\r\n")
	buf.WriteString(fmt.Sprintf("Content-Type: multipart/mixed; boundary=%q\r\n", boundary))
	buf.WriteString("\r\n")

	// Text body part
	buf.WriteString(fmt.Sprintf("--%s\r\n", boundary))
	buf.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	buf.WriteString("\r\n")
	buf.WriteString(body)
	buf.WriteString("\r\n")

	// Attachment parts
	for _, att := range attachments {
		buf.WriteString(fmt.Sprintf("--%s\r\n", boundary))
		buf.WriteString(fmt.Sprintf("Content-Type: %s; name=%q\r\n", att.MimeType, att.Filename))
		buf.WriteString(fmt.Sprintf("Content-Disposition: attachment; filename=%q\r\n", att.Filename))
		buf.WriteString("Content-Transfer-Encoding: base64\r\n")
		buf.WriteString("\r\n")
		buf.WriteString(wrapBase64Lines(att.Data))
		buf.WriteString("\r\n")
	}

	// Closing boundary
	buf.WriteString(fmt.Sprintf("--%s--\r\n", boundary))

	return base64.RawURLEncoding.EncodeToString([]byte(buf.String()))
}

func generateBoundary() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return fmt.Sprintf("__mcp_gcal_%x", b)
}

// wrapBase64Lines strips whitespace from base64 data and inserts CRLF every 76 characters per MIME spec.
func wrapBase64Lines(data string) string {
	// Strip existing whitespace
	var clean strings.Builder
	for _, r := range data {
		if r != ' ' && r != '\n' && r != '\r' && r != '\t' {
			clean.WriteRune(r)
		}
	}
	s := clean.String()

	var buf strings.Builder
	for i := 0; i < len(s); i += 76 {
		end := i + 76
		if end > len(s) {
			end = len(s)
		}
		buf.WriteString(s[i:end])
		buf.WriteString("\r\n")
	}
	return strings.TrimRight(buf.String(), "\r\n")
}

// Service methods

// SearchEmails searches emails using Gmail query syntax and returns metadata.
func (gs *GmailService) SearchEmails(query string, maxResults int64) ([]emailJSON, error) {
	if maxResults <= 0 {
		maxResults = 20
	}

	list, err := gs.svc.Users.Messages.List("me").Q(query).MaxResults(maxResults).Do()
	if err != nil {
		return nil, fmt.Errorf("search emails: %w", err)
	}

	results := make([]emailJSON, 0, len(list.Messages))
	for _, m := range list.Messages {
		msg, err := gs.svc.Users.Messages.Get("me", m.Id).Format("metadata").
			MetadataHeaders("Subject", "From", "To", "Date").Do()
		if err != nil {
			continue
		}
		email := emailJSON{
			ID:       msg.Id,
			ThreadID: msg.ThreadId,
			Snippet:  msg.Snippet,
			Labels:   msg.LabelIds,
		}
		if msg.Payload != nil {
			email.Subject = getHeader(msg.Payload.Headers, "Subject")
			email.From = getHeader(msg.Payload.Headers, "From")
			email.To = getHeader(msg.Payload.Headers, "To")
			email.Date = getHeader(msg.Payload.Headers, "Date")
		}
		results = append(results, email)
	}
	return results, nil
}

// ReadEmail retrieves the full content of an email.
func (gs *GmailService) ReadEmail(messageID string) (*emailJSON, error) {
	msg, err := gs.svc.Users.Messages.Get("me", messageID).Format("full").Do()
	if err != nil {
		return nil, fmt.Errorf("read email: %w", err)
	}
	email := convertMessage(msg)
	return &email, nil
}

// SendEmail sends an email and returns the sent message metadata.
func (gs *GmailService) SendEmail(to, subject, body, cc, bcc, threadID, inReplyTo string, attachments []Attachment) (*emailJSON, error) {
	raw := buildRawEmail(to, subject, body, cc, bcc, inReplyTo, attachments)
	msg := &gmail.Message{Raw: raw}
	if threadID != "" {
		msg.ThreadId = threadID
	}

	sent, err := gs.svc.Users.Messages.Send("me", msg).Do()
	if err != nil {
		return nil, fmt.Errorf("send email: %w", err)
	}

	// Fetch metadata of the sent message
	result, err := gs.svc.Users.Messages.Get("me", sent.Id).Format("metadata").
		MetadataHeaders("Subject", "From", "To", "Date").Do()
	if err != nil {
		return &emailJSON{ID: sent.Id, ThreadID: sent.ThreadId}, nil
	}
	email := emailJSON{
		ID:       result.Id,
		ThreadID: result.ThreadId,
		Labels:   result.LabelIds,
	}
	if result.Payload != nil {
		email.Subject = getHeader(result.Payload.Headers, "Subject")
		email.From = getHeader(result.Payload.Headers, "From")
		email.To = getHeader(result.Payload.Headers, "To")
		email.Date = getHeader(result.Payload.Headers, "Date")
	}
	return &email, nil
}

// DraftEmail creates a draft email without sending it.
func (gs *GmailService) DraftEmail(to, subject, body, cc, bcc string, attachments []Attachment) (any, error) {
	raw := buildRawEmail(to, subject, body, cc, bcc, "", attachments)
	draft := &gmail.Draft{
		Message: &gmail.Message{Raw: raw},
	}

	created, err := gs.svc.Users.Drafts.Create("me", draft).Do()
	if err != nil {
		return nil, fmt.Errorf("create draft: %w", err)
	}
	return map[string]string{
		"status":     "drafted",
		"draft_id":   created.Id,
		"message_id": created.Message.Id,
	}, nil
}

// ModifyEmail adds or removes labels on an email.
func (gs *GmailService) ModifyEmail(messageID, addLabels, removeLabels string) (*emailJSON, error) {
	req := &gmail.ModifyMessageRequest{}
	if addLabels != "" {
		for _, l := range strings.Split(addLabels, ",") {
			l = strings.TrimSpace(l)
			if l != "" {
				req.AddLabelIds = append(req.AddLabelIds, l)
			}
		}
	}
	if removeLabels != "" {
		for _, l := range strings.Split(removeLabels, ",") {
			l = strings.TrimSpace(l)
			if l != "" {
				req.RemoveLabelIds = append(req.RemoveLabelIds, l)
			}
		}
	}

	msg, err := gs.svc.Users.Messages.Modify("me", messageID, req).Do()
	if err != nil {
		return nil, fmt.Errorf("modify email: %w", err)
	}
	return &emailJSON{
		ID:       msg.Id,
		ThreadID: msg.ThreadId,
		Labels:   msg.LabelIds,
	}, nil
}

// DeleteEmail moves an email to trash.
func (gs *GmailService) DeleteEmail(messageID string) error {
	_, err := gs.svc.Users.Messages.Trash("me", messageID).Do()
	if err != nil {
		return fmt.Errorf("trash email: %w", err)
	}
	return nil
}

// ListLabels returns all Gmail labels.
func (gs *GmailService) ListLabels() ([]labelJSON, error) {
	list, err := gs.svc.Users.Labels.List("me").Do()
	if err != nil {
		return nil, fmt.Errorf("list labels: %w", err)
	}
	result := make([]labelJSON, 0, len(list.Labels))
	for _, l := range list.Labels {
		result = append(result, labelJSON{
			ID:             l.Id,
			Name:           l.Name,
			Type:           l.Type,
			MessagesTotal:  l.MessagesTotal,
			MessagesUnread: l.MessagesUnread,
		})
	}
	return result, nil
}
