package main

import (
	"net/http"
	"testing"
)

func TestResolveBaseURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		addr    string
		baseURL string
		want    string
		wantErr bool
	}{
		{
			name:    "explicit base url trims trailing slash",
			addr:    ":8080",
			baseURL: "https://example.com/",
			want:    "https://example.com",
		},
		{
			name:    "explicit base url with query is rejected",
			addr:    ":8080",
			baseURL: "https://example.com?a=1",
			wantErr: true,
		},
		{
			name:    "explicit base url with invalid scheme is rejected",
			addr:    ":8080",
			baseURL: "ftp://example.com",
			wantErr: true,
		},
		{
			name: "derive from wildcard addr",
			addr: ":8080",
			want: "http://localhost:8080",
		},
		{
			name: "derive from ipv4 wildcard addr",
			addr: "0.0.0.0:9000",
			want: "http://localhost:9000",
		},
		{
			name: "derive from explicit host",
			addr: "127.0.0.1:7000",
			want: "http://127.0.0.1:7000",
		},
		{
			name:    "invalid addr without host separator",
			addr:    "8080",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := resolveBaseURL(tt.addr, tt.baseURL)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("resolveBaseURL() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestExtractBearerToken(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		header string
		want   string
	}{
		{
			name:   "valid bearer",
			header: "Bearer abc123",
			want:   "abc123",
		},
		{
			name:   "case insensitive scheme",
			header: "bearer xyz",
			want:   "xyz",
		},
		{
			name:   "wrong scheme",
			header: "Basic abc",
			want:   "",
		},
		{
			name:   "missing value",
			header: "Bearer",
			want:   "",
		},
		{
			name:   "empty header",
			header: "",
			want:   "",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req, err := http.NewRequest(http.MethodPost, "http://example.com/mcp", nil)
			if err != nil {
				t.Fatalf("new request: %v", err)
			}
			if tt.header != "" {
				req.Header.Set("Authorization", tt.header)
			}
			got := extractBearerToken(req)
			if got != tt.want {
				t.Fatalf("extractBearerToken() = %q, want %q", got, tt.want)
			}
		})
	}
}
