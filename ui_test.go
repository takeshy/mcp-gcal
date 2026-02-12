package main

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestUIResourceURI(t *testing.T) {
	t.Parallel()

	got := uiResourceURI("show-calendar")
	want := "ui://show-calendar/result"
	if got != want {
		t.Fatalf("uiResourceURI() = %q, want %q", got, want)
	}
}

func TestBuildToolMeta_WithUI(t *testing.T) {
	t.Parallel()

	tool := mcpTool{
		Name:       "show-calendar",
		uiTemplate: "templates/calendar.html",
		visibility: []string{"model", "app"},
	}
	meta := buildToolMeta(tool)
	if meta == nil {
		t.Fatal("buildToolMeta() returned nil for UI tool")
	}

	ui, ok := meta["ui"].(map[string]interface{})
	if !ok {
		t.Fatal("meta[\"ui\"] is not a map")
	}

	uri, ok := ui["resourceUri"].(string)
	if !ok || uri != "ui://show-calendar/result" {
		t.Fatalf("resourceUri = %q, want %q", uri, "ui://show-calendar/result")
	}

	vis, ok := ui["visibility"].([]string)
	if !ok || len(vis) != 2 {
		t.Fatalf("visibility = %v, want [model app]", vis)
	}
}

func TestBuildToolMeta_WithoutUI(t *testing.T) {
	t.Parallel()

	tool := mcpTool{Name: "list-events"}
	meta := buildToolMeta(tool)
	if meta != nil {
		t.Fatalf("buildToolMeta() = %v, want nil for non-UI tool", meta)
	}
}

func TestBuildResultMeta_WithUI(t *testing.T) {
	t.Parallel()

	tool := mcpTool{
		Name:       "show-calendar",
		uiTemplate: "templates/calendar.html",
	}
	output := `[{"id":"1","summary":"Meeting"}]`
	meta := buildResultMeta(tool, output)
	if meta == nil {
		t.Fatal("buildResultMeta() returned nil")
	}

	ui, ok := meta["ui"].(map[string]interface{})
	if !ok {
		t.Fatal("meta[\"ui\"] is not a map")
	}

	uri, ok := ui["resourceUri"].(string)
	if !ok {
		t.Fatal("resourceUri is not a string")
	}
	if !strings.HasPrefix(uri, "ui://show-calendar/result?data=") {
		t.Fatalf("resourceUri = %q, want prefix %q", uri, "ui://show-calendar/result?data=")
	}

	// Verify the encoded data
	dataParam := strings.TrimPrefix(uri, "ui://show-calendar/result?data=")
	decoded, err := base64.URLEncoding.DecodeString(dataParam)
	if err != nil {
		t.Fatalf("decode data param: %v", err)
	}
	if string(decoded) != output {
		t.Fatalf("decoded data = %q, want %q", string(decoded), output)
	}
}

func TestBuildResultMeta_WithoutUI(t *testing.T) {
	t.Parallel()

	tool := mcpTool{Name: "list-events"}
	meta := buildResultMeta(tool, `{"result":"ok"}`)
	if meta != nil {
		t.Fatalf("buildResultMeta() = %v, want nil for non-UI tool", meta)
	}
}

func TestParseUIResourceURI(t *testing.T) {
	t.Parallel()

	data := base64.URLEncoding.EncodeToString([]byte(`[{"id":"1"}]`))
	uri := "ui://show-calendar/result?data=" + data

	tool, encodedData, err := parseUIResourceURI(uri)
	if err != nil {
		t.Fatalf("parseUIResourceURI() error = %v", err)
	}
	if tool.Name != "show-calendar" {
		t.Fatalf("tool.Name = %q, want %q", tool.Name, "show-calendar")
	}
	if encodedData != data {
		t.Fatalf("encodedData = %q, want %q", encodedData, data)
	}
}

func TestParseUIResourceURI_NoData(t *testing.T) {
	t.Parallel()

	tool, data, err := parseUIResourceURI("ui://show-calendar/result")
	if err != nil {
		t.Fatalf("parseUIResourceURI() error = %v", err)
	}
	if tool.Name != "show-calendar" {
		t.Fatalf("tool.Name = %q, want %q", tool.Name, "show-calendar")
	}
	if data != "" {
		t.Fatalf("data = %q, want empty", data)
	}
}

func TestParseUIResourceURI_InvalidScheme(t *testing.T) {
	t.Parallel()

	_, _, err := parseUIResourceURI("http://show-calendar/result")
	if err == nil {
		t.Fatal("expected error for non-ui scheme")
	}
	if !strings.Contains(err.Error(), "unsupported scheme") {
		t.Fatalf("error = %v, want 'unsupported scheme'", err)
	}
}

func TestParseUIResourceURI_UnknownTool(t *testing.T) {
	t.Parallel()

	_, _, err := parseUIResourceURI("ui://nonexistent-tool/result")
	if err == nil {
		t.Fatal("expected error for unknown tool")
	}
}

func TestParseUIResourceURI_NonUITool(t *testing.T) {
	t.Parallel()

	_, _, err := parseUIResourceURI("ui://list-events/result")
	if err == nil {
		t.Fatal("expected error for tool without UI")
	}
}

func TestFindTool(t *testing.T) {
	t.Parallel()

	tool := findTool("show-calendar")
	if tool == nil {
		t.Fatal("findTool(\"show-calendar\") returned nil")
	}
	if tool.Name != "show-calendar" {
		t.Fatalf("tool.Name = %q, want %q", tool.Name, "show-calendar")
	}

	tool = findTool("search-emails")
	if tool == nil {
		t.Fatal("findTool(\"search-emails\") returned nil")
	}

	tool = findTool("nonexistent")
	if tool != nil {
		t.Fatalf("findTool(\"nonexistent\") = %v, want nil", tool)
	}
}

func TestGenerateUIHTML(t *testing.T) {
	t.Parallel()

	tool := mcpTool{
		Name:       "show-calendar",
		uiTemplate: "templates/calendar.html",
	}
	jsonData := `[{"id":"1","summary":"Test Event","start":{"dateTime":"2024-01-01T10:00:00Z"}}]`
	encodedData := base64.URLEncoding.EncodeToString([]byte(jsonData))

	html, err := generateUIHTML(tool, encodedData, "session-123")
	if err != nil {
		t.Fatalf("generateUIHTML() error = %v", err)
	}
	if html == "" {
		t.Fatal("generateUIHTML() returned empty string")
	}
	if !strings.Contains(html, "<") {
		t.Fatal("output doesn't look like HTML")
	}
}

func TestGenerateUIHTML_UnknownTemplate(t *testing.T) {
	t.Parallel()

	tool := mcpTool{
		Name:       "test",
		uiTemplate: "templates/unknown.html",
	}
	encodedData := base64.URLEncoding.EncodeToString([]byte("{}"))

	_, err := generateUIHTML(tool, encodedData, "")
	if err == nil {
		t.Fatal("expected error for unknown template")
	}
}
