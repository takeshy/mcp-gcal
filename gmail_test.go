package main

import (
	"encoding/base64"
	"strings"
	"testing"

	"google.golang.org/api/gmail/v1"
)

func TestGetHeader(t *testing.T) {
	t.Parallel()

	headers := []*gmail.MessagePartHeader{
		{Name: "Subject", Value: "Hello"},
		{Name: "From", Value: "alice@example.com"},
		{Name: "To", Value: "bob@example.com"},
	}

	tests := []struct {
		name   string
		header string
		want   string
	}{
		{"exact match", "Subject", "Hello"},
		{"case insensitive", "subject", "Hello"},
		{"another header", "From", "alice@example.com"},
		{"missing header", "Cc", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := getHeader(headers, tt.header)
			if got != tt.want {
				t.Fatalf("getHeader(%q) = %q, want %q", tt.header, got, tt.want)
			}
		})
	}
}

func TestGetHeader_Nil(t *testing.T) {
	t.Parallel()
	got := getHeader(nil, "Subject")
	if got != "" {
		t.Fatalf("getHeader(nil) = %q, want empty", got)
	}
}

func b64(s string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(s))
}

func TestExtractEmailBody_PlainText(t *testing.T) {
	t.Parallel()

	part := &gmail.MessagePart{
		MimeType: "text/plain",
		Body:     &gmail.MessagePartBody{Data: b64("Hello world")},
	}
	text, html := extractEmailBody(part)
	if text != "Hello world" {
		t.Fatalf("text = %q, want %q", text, "Hello world")
	}
	if html != "" {
		t.Fatalf("html = %q, want empty", html)
	}
}

func TestExtractEmailBody_HTML(t *testing.T) {
	t.Parallel()

	part := &gmail.MessagePart{
		MimeType: "text/html",
		Body:     &gmail.MessagePartBody{Data: b64("<p>Hello</p>")},
	}
	text, html := extractEmailBody(part)
	if text != "" {
		t.Fatalf("text = %q, want empty", text)
	}
	if html != "<p>Hello</p>" {
		t.Fatalf("html = %q, want %q", html, "<p>Hello</p>")
	}
}

func TestExtractEmailBody_Multipart(t *testing.T) {
	t.Parallel()

	part := &gmail.MessagePart{
		MimeType: "multipart/alternative",
		Parts: []*gmail.MessagePart{
			{
				MimeType: "text/plain",
				Body:     &gmail.MessagePartBody{Data: b64("plain text")},
			},
			{
				MimeType: "text/html",
				Body:     &gmail.MessagePartBody{Data: b64("<b>html</b>")},
			},
		},
	}
	text, html := extractEmailBody(part)
	if text != "plain text" {
		t.Fatalf("text = %q, want %q", text, "plain text")
	}
	if html != "<b>html</b>" {
		t.Fatalf("html = %q, want %q", html, "<b>html</b>")
	}
}

func TestExtractEmailBody_NestedMultipart(t *testing.T) {
	t.Parallel()

	part := &gmail.MessagePart{
		MimeType: "multipart/mixed",
		Parts: []*gmail.MessagePart{
			{
				MimeType: "multipart/alternative",
				Parts: []*gmail.MessagePart{
					{
						MimeType: "text/plain",
						Body:     &gmail.MessagePartBody{Data: b64("nested plain")},
					},
					{
						MimeType: "text/html",
						Body:     &gmail.MessagePartBody{Data: b64("<i>nested html</i>")},
					},
				},
			},
			{
				MimeType: "application/pdf",
				Filename: "doc.pdf",
				Body:     &gmail.MessagePartBody{AttachmentId: "att1", Size: 1024},
			},
		},
	}
	text, html := extractEmailBody(part)
	if text != "nested plain" {
		t.Fatalf("text = %q, want %q", text, "nested plain")
	}
	if html != "<i>nested html</i>" {
		t.Fatalf("html = %q, want %q", html, "<i>nested html</i>")
	}
}

func TestExtractEmailBody_Nil(t *testing.T) {
	t.Parallel()
	text, html := extractEmailBody(nil)
	if text != "" || html != "" {
		t.Fatalf("extractEmailBody(nil) = (%q, %q), want empty", text, html)
	}
}

func TestExtractEmailBody_EmptyBody(t *testing.T) {
	t.Parallel()

	part := &gmail.MessagePart{
		MimeType: "text/plain",
		Body:     &gmail.MessagePartBody{Data: ""},
	}
	text, html := extractEmailBody(part)
	if text != "" || html != "" {
		t.Fatalf("got (%q, %q), want empty", text, html)
	}
}

func TestExtractEmailBody_MultipartHTMLOnly(t *testing.T) {
	t.Parallel()

	part := &gmail.MessagePart{
		MimeType: "multipart/alternative",
		Parts: []*gmail.MessagePart{
			{
				MimeType: "text/html",
				Body:     &gmail.MessagePartBody{Data: b64("<p>only html</p>")},
			},
		},
	}
	text, html := extractEmailBody(part)
	if text != "" {
		t.Fatalf("text = %q, want empty", text)
	}
	if html != "<p>only html</p>" {
		t.Fatalf("html = %q, want %q", html, "<p>only html</p>")
	}
}

func TestExtractAttachments(t *testing.T) {
	t.Parallel()

	part := &gmail.MessagePart{
		MimeType: "multipart/mixed",
		Parts: []*gmail.MessagePart{
			{
				MimeType: "text/plain",
				Body:     &gmail.MessagePartBody{Data: b64("body")},
			},
			{
				MimeType: "application/pdf",
				Filename: "report.pdf",
				Body:     &gmail.MessagePartBody{AttachmentId: "att1", Size: 2048},
			},
			{
				MimeType: "image/png",
				Filename: "photo.png",
				Body:     &gmail.MessagePartBody{AttachmentId: "att2", Size: 4096},
			},
		},
	}

	attachments := extractAttachments(part)
	if len(attachments) != 2 {
		t.Fatalf("got %d attachments, want 2", len(attachments))
	}
	if attachments[0].Filename != "report.pdf" {
		t.Fatalf("attachments[0].Filename = %q, want %q", attachments[0].Filename, "report.pdf")
	}
	if attachments[0].MimeType != "application/pdf" {
		t.Fatalf("attachments[0].MimeType = %q, want %q", attachments[0].MimeType, "application/pdf")
	}
	if attachments[0].Size != 2048 {
		t.Fatalf("attachments[0].Size = %d, want 2048", attachments[0].Size)
	}
	if attachments[1].Filename != "photo.png" {
		t.Fatalf("attachments[1].Filename = %q, want %q", attachments[1].Filename, "photo.png")
	}
}

func TestExtractAttachments_Nil(t *testing.T) {
	t.Parallel()
	attachments := extractAttachments(nil)
	if attachments != nil {
		t.Fatalf("extractAttachments(nil) = %v, want nil", attachments)
	}
}

func TestExtractAttachments_NoAttachments(t *testing.T) {
	t.Parallel()

	part := &gmail.MessagePart{
		MimeType: "text/plain",
		Body:     &gmail.MessagePartBody{Data: b64("just text")},
	}
	attachments := extractAttachments(part)
	if len(attachments) != 0 {
		t.Fatalf("got %d attachments, want 0", len(attachments))
	}
}

func TestConvertMessage(t *testing.T) {
	t.Parallel()

	msg := &gmail.Message{
		Id:       "msg123",
		ThreadId: "thread456",
		Snippet:  "Preview text...",
		LabelIds: []string{"INBOX", "UNREAD"},
		Payload: &gmail.MessagePart{
			MimeType: "multipart/alternative",
			Headers: []*gmail.MessagePartHeader{
				{Name: "Subject", Value: "Test Subject"},
				{Name: "From", Value: "sender@example.com"},
				{Name: "To", Value: "recipient@example.com"},
				{Name: "Cc", Value: "cc@example.com"},
				{Name: "Date", Value: "Mon, 1 Jan 2024 12:00:00 +0000"},
			},
			Parts: []*gmail.MessagePart{
				{
					MimeType: "text/plain",
					Body:     &gmail.MessagePartBody{Data: b64("Hello from test")},
				},
				{
					MimeType: "application/pdf",
					Filename: "file.pdf",
					Body:     &gmail.MessagePartBody{AttachmentId: "att1", Size: 512},
				},
			},
		},
	}

	email := convertMessage(msg)

	if email.ID != "msg123" {
		t.Fatalf("ID = %q, want %q", email.ID, "msg123")
	}
	if email.ThreadID != "thread456" {
		t.Fatalf("ThreadID = %q, want %q", email.ThreadID, "thread456")
	}
	if email.Subject != "Test Subject" {
		t.Fatalf("Subject = %q, want %q", email.Subject, "Test Subject")
	}
	if email.From != "sender@example.com" {
		t.Fatalf("From = %q, want %q", email.From, "sender@example.com")
	}
	if email.To != "recipient@example.com" {
		t.Fatalf("To = %q, want %q", email.To, "recipient@example.com")
	}
	if email.Cc != "cc@example.com" {
		t.Fatalf("Cc = %q, want %q", email.Cc, "cc@example.com")
	}
	if email.Body != "Hello from test" {
		t.Fatalf("Body = %q, want %q", email.Body, "Hello from test")
	}
	if len(email.Attachments) != 1 {
		t.Fatalf("got %d attachments, want 1", len(email.Attachments))
	}
	if email.Attachments[0].Filename != "file.pdf" {
		t.Fatalf("attachment filename = %q, want %q", email.Attachments[0].Filename, "file.pdf")
	}
}

func TestConvertMessage_NilPayload(t *testing.T) {
	t.Parallel()

	msg := &gmail.Message{
		Id:       "msg1",
		ThreadId: "t1",
		Snippet:  "snippet",
	}
	email := convertMessage(msg)
	if email.ID != "msg1" {
		t.Fatalf("ID = %q, want %q", email.ID, "msg1")
	}
	if email.Body != "" {
		t.Fatalf("Body = %q, want empty", email.Body)
	}
}

func TestConvertMessage_HTMLFallback(t *testing.T) {
	t.Parallel()

	msg := &gmail.Message{
		Id:       "msg2",
		ThreadId: "t2",
		Payload: &gmail.MessagePart{
			MimeType: "text/html",
			Headers:  []*gmail.MessagePartHeader{{Name: "Subject", Value: "HTML only"}},
			Body:     &gmail.MessagePartBody{Data: b64("<h1>Title</h1>")},
		},
	}
	email := convertMessage(msg)
	if email.Body != "<h1>Title</h1>" {
		t.Fatalf("Body = %q, want %q", email.Body, "<h1>Title</h1>")
	}
}

func TestValidateAttachments_Valid(t *testing.T) {
	t.Parallel()
	err := validateAttachments([]Attachment{
		{Filename: "doc.pdf", MimeType: "application/pdf", Data: "AQID"},
		{Filename: "img.png", MimeType: "image/png", Data: "abc123"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateAttachments_Nil(t *testing.T) {
	t.Parallel()
	if err := validateAttachments(nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateAttachments_EmptyFilename(t *testing.T) {
	t.Parallel()
	err := validateAttachments([]Attachment{
		{Filename: "", MimeType: "application/pdf", Data: "AQID"},
	})
	if err == nil {
		t.Fatal("expected error for empty filename")
	}
	if !contains(err.Error(), "filename is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateAttachments_EmptyMimeType(t *testing.T) {
	t.Parallel()
	err := validateAttachments([]Attachment{
		{Filename: "doc.pdf", MimeType: "", Data: "AQID"},
	})
	if err == nil {
		t.Fatal("expected error for empty mime_type")
	}
	if !contains(err.Error(), "mime_type is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateAttachments_CRLFInMimeType(t *testing.T) {
	t.Parallel()
	err := validateAttachments([]Attachment{
		{Filename: "doc.pdf", MimeType: "application/pdf\r\nX-Injected: evil", Data: "AQID"},
	})
	if err == nil {
		t.Fatal("expected error for CRLF in mime_type")
	}
	if !contains(err.Error(), "invalid characters") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateAttachments_InvalidMimeTypeFormat(t *testing.T) {
	t.Parallel()
	err := validateAttachments([]Attachment{
		{Filename: "doc.pdf", MimeType: "not-a-mime-type", Data: "AQID"},
	})
	if err == nil {
		t.Fatal("expected error for invalid mime_type format")
	}
	if !contains(err.Error(), "type/subtype format") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateAttachments_EmptyData(t *testing.T) {
	t.Parallel()
	err := validateAttachments([]Attachment{
		{Filename: "doc.pdf", MimeType: "application/pdf", Data: ""},
	})
	if err == nil {
		t.Fatal("expected error for empty data")
	}
	if !contains(err.Error(), "data is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateAttachments_SecondItemInvalid(t *testing.T) {
	t.Parallel()
	err := validateAttachments([]Attachment{
		{Filename: "ok.pdf", MimeType: "application/pdf", Data: "AQID"},
		{Filename: "bad.txt", MimeType: "text", Data: "abc"},
	})
	if err == nil {
		t.Fatal("expected error for invalid second attachment")
	}
	if !contains(err.Error(), "attachment[1]") {
		t.Fatalf("expected error to reference attachment[1]: %v", err)
	}
}

func TestBuildRawEmail(t *testing.T) {
	t.Parallel()

	raw := buildRawEmail("to@example.com", "Test Subject", "Hello body", "", "", "", nil)
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		t.Fatalf("decode raw email: %v", err)
	}
	s := string(decoded)

	if !contains(s, "To: to@example.com\r\n") {
		t.Fatalf("missing To header in: %s", s)
	}
	if !contains(s, "Content-Type: text/plain; charset=UTF-8\r\n") {
		t.Fatalf("missing Content-Type in: %s", s)
	}
	if !contains(s, "Hello body") {
		t.Fatalf("missing body in: %s", s)
	}
	// No Cc/Bcc when empty
	if contains(s, "Cc:") {
		t.Fatalf("unexpected Cc header in: %s", s)
	}
	if contains(s, "Bcc:") {
		t.Fatalf("unexpected Bcc header in: %s", s)
	}
	// No MIME-Version for simple email
	if contains(s, "MIME-Version:") {
		t.Fatalf("unexpected MIME-Version header for simple email in: %s", s)
	}
}

func TestBuildRawEmail_WithCcBcc(t *testing.T) {
	t.Parallel()

	raw := buildRawEmail("to@example.com", "Subject", "Body", "cc@example.com", "bcc@example.com", "", nil)
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		t.Fatalf("decode raw email: %v", err)
	}
	s := string(decoded)

	if !contains(s, "Cc: cc@example.com\r\n") {
		t.Fatalf("missing Cc header in: %s", s)
	}
	if !contains(s, "Bcc: bcc@example.com\r\n") {
		t.Fatalf("missing Bcc header in: %s", s)
	}
}

func TestBuildRawEmail_WithInReplyTo(t *testing.T) {
	t.Parallel()

	raw := buildRawEmail("to@example.com", "Re: Subject", "Reply body", "", "", "<msg-id@example.com>", nil)
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		t.Fatalf("decode raw email: %v", err)
	}
	s := string(decoded)

	if !contains(s, "In-Reply-To: <msg-id@example.com>\r\n") {
		t.Fatalf("missing In-Reply-To in: %s", s)
	}
	if !contains(s, "References: <msg-id@example.com>\r\n") {
		t.Fatalf("missing References in: %s", s)
	}
}

func TestBuildRawEmail_UTF8Subject(t *testing.T) {
	t.Parallel()

	raw := buildRawEmail("to@example.com", "日本語の件名", "本文", "", "", "", nil)
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		t.Fatalf("decode raw email: %v", err)
	}
	s := string(decoded)

	// Subject should be Q-encoded for UTF-8
	if contains(s, "Subject: 日本語の件名") {
		t.Fatalf("subject should be Q-encoded, got raw UTF-8 in: %s", s)
	}
	if !contains(s, "Subject: =?utf-8?") {
		t.Fatalf("missing Q-encoded subject in: %s", s)
	}
}

func TestBuildRawEmail_WithAttachments(t *testing.T) {
	t.Parallel()

	fileData := base64.StdEncoding.EncodeToString([]byte("PDF file content"))
	attachments := []Attachment{
		{
			Filename: "report.pdf",
			MimeType: "application/pdf",
			Data:     fileData,
		},
	}

	raw := buildRawEmail("to@example.com", "With Attachment", "See attached.", "", "", "", attachments)
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		t.Fatalf("decode raw email: %v", err)
	}
	s := string(decoded)

	if !contains(s, "MIME-Version: 1.0\r\n") {
		t.Fatalf("missing MIME-Version in: %s", s)
	}
	if !contains(s, "Content-Type: multipart/mixed; boundary=") {
		t.Fatalf("missing multipart Content-Type in: %s", s)
	}
	if !contains(s, "Content-Type: text/plain; charset=UTF-8\r\n") {
		t.Fatalf("missing text part Content-Type in: %s", s)
	}
	if !contains(s, "See attached.") {
		t.Fatalf("missing body text in: %s", s)
	}
	if !contains(s, `Content-Type: application/pdf; name="report.pdf"`) {
		t.Fatalf("missing attachment Content-Type in: %s", s)
	}
	if !contains(s, `Content-Disposition: attachment; filename="report.pdf"`) {
		t.Fatalf("missing Content-Disposition in: %s", s)
	}
	if !contains(s, "Content-Transfer-Encoding: base64\r\n") {
		t.Fatalf("missing Content-Transfer-Encoding in: %s", s)
	}
	if !contains(s, fileData) {
		t.Fatalf("missing attachment data in: %s", s)
	}
}

func TestBuildRawEmail_MultipleAttachments(t *testing.T) {
	t.Parallel()

	attachments := []Attachment{
		{
			Filename: "doc.pdf",
			MimeType: "application/pdf",
			Data:     base64.StdEncoding.EncodeToString([]byte("pdf")),
		},
		{
			Filename: "image.png",
			MimeType: "image/png",
			Data:     base64.StdEncoding.EncodeToString([]byte("png")),
		},
	}

	raw := buildRawEmail("to@example.com", "Multi", "Body", "", "", "", attachments)
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		t.Fatalf("decode raw email: %v", err)
	}
	s := string(decoded)

	if !contains(s, `filename="doc.pdf"`) {
		t.Fatalf("missing first attachment in: %s", s)
	}
	if !contains(s, `filename="image.png"`) {
		t.Fatalf("missing second attachment in: %s", s)
	}
}

func TestBuildRawEmail_EmptyAttachments(t *testing.T) {
	t.Parallel()

	// Empty slice should produce simple email (no MIME multipart)
	raw := buildRawEmail("to@example.com", "Simple", "Body", "", "", "", []Attachment{})
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		t.Fatalf("decode raw email: %v", err)
	}
	s := string(decoded)

	if contains(s, "MIME-Version:") {
		t.Fatalf("unexpected MIME-Version for empty attachments in: %s", s)
	}
	if contains(s, "multipart/mixed") {
		t.Fatalf("unexpected multipart for empty attachments in: %s", s)
	}
}

func TestWrapBase64Lines(t *testing.T) {
	t.Parallel()

	// Short data - no wrapping needed
	short := "SGVsbG8="
	result := wrapBase64Lines(short)
	if result != short {
		t.Fatalf("wrapBase64Lines(%q) = %q, want %q", short, result, short)
	}

	// Data longer than 76 chars should be wrapped
	long := ""
	for i := 0; i < 100; i++ {
		long += "A"
	}
	result = wrapBase64Lines(long)
	lines := splitLines(result)
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d: %q", len(lines), result)
	}
	if len(lines[0]) != 76 {
		t.Fatalf("first line length = %d, want 76", len(lines[0]))
	}
	if len(lines[1]) != 24 {
		t.Fatalf("second line length = %d, want 24", len(lines[1]))
	}
}

// splitLines splits on CRLF, filtering empty trailing entries.
func splitLines(s string) []string {
	parts := strings.Split(s, "\r\n")
	var result []string
	for _, p := range parts {
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
