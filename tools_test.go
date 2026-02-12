package main

import (
	"testing"
)

func TestIsGmailTool(t *testing.T) {
	t.Parallel()

	gmailTools := []string{
		"search-emails", "read-email", "send-email", "draft-email",
		"modify-email", "delete-email", "list-email-labels",
	}
	for _, name := range gmailTools {
		if !isGmailTool(name) {
			t.Errorf("isGmailTool(%q) = false, want true", name)
		}
	}

	calendarTools := []string{
		"list-calendars", "list-events", "get-event", "search-events",
		"create-event", "update-event", "delete-event", "respond-to-event",
		"show-calendar", "authenticate",
	}
	for _, name := range calendarTools {
		if isGmailTool(name) {
			t.Errorf("isGmailTool(%q) = true, want false", name)
		}
	}

	if isGmailTool("unknown-tool") {
		t.Error("isGmailTool(\"unknown-tool\") = true, want false")
	}
}

func TestIsVisibleToModel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		visibility []string
		want       bool
	}{
		{"no visibility (default)", nil, true},
		{"empty visibility", []string{}, true},
		{"model only", []string{"model"}, true},
		{"model and app", []string{"model", "app"}, true},
		{"app only", []string{"app"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tool := mcpTool{Name: "test", visibility: tt.visibility}
			got := tool.isVisibleToModel()
			if got != tt.want {
				t.Fatalf("isVisibleToModel() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestHasUI(t *testing.T) {
	t.Parallel()

	withUI := mcpTool{Name: "test", uiTemplate: "templates/calendar.html"}
	if !withUI.hasUI() {
		t.Fatal("hasUI() = false for tool with uiTemplate")
	}

	withoutUI := mcpTool{Name: "test"}
	if withoutUI.hasUI() {
		t.Fatal("hasUI() = true for tool without uiTemplate")
	}
}

func TestAllTools_ContainsExpectedTools(t *testing.T) {
	t.Parallel()

	tools := allTools()
	names := make(map[string]bool)
	for _, tool := range tools {
		names[tool.Name] = true
	}

	expected := []string{
		"authenticate", "list-calendars", "list-events", "get-event",
		"search-events", "create-event", "update-event", "delete-event",
		"respond-to-event", "show-calendar",
		"search-emails", "read-email", "send-email", "draft-email",
		"modify-email", "delete-email", "list-email-labels",
		"gcal-list-events-app", "gcal-create-event-app",
		"gcal-delete-event-app", "gcal-get-event-app",
	}

	for _, name := range expected {
		if !names[name] {
			t.Errorf("allTools() missing %q", name)
		}
	}
}

func TestAllTools_NoDuplicateNames(t *testing.T) {
	t.Parallel()

	tools := allTools()
	seen := make(map[string]bool)
	for _, tool := range tools {
		if seen[tool.Name] {
			t.Errorf("duplicate tool name: %q", tool.Name)
		}
		seen[tool.Name] = true
	}
}

func TestAllTools_AppOnlyToolsNotVisibleToModel(t *testing.T) {
	t.Parallel()

	appOnly := []string{
		"gcal-list-events-app", "gcal-create-event-app",
		"gcal-delete-event-app", "gcal-get-event-app",
	}
	appOnlySet := make(map[string]bool)
	for _, name := range appOnly {
		appOnlySet[name] = true
	}

	for _, tool := range allTools() {
		if appOnlySet[tool.Name] && tool.isVisibleToModel() {
			t.Errorf("app-only tool %q should not be visible to model", tool.Name)
		}
	}
}

func TestAllTools_RequiredFieldsPresent(t *testing.T) {
	t.Parallel()

	for _, tool := range allTools() {
		if tool.Name == "" {
			t.Error("tool with empty Name")
		}
		if tool.Description == "" {
			t.Errorf("tool %q has empty Description", tool.Name)
		}
		if tool.InputSchema.Type != "object" {
			t.Errorf("tool %q InputSchema.Type = %q, want %q", tool.Name, tool.InputSchema.Type, "object")
		}
	}
}

func TestArgHelpers(t *testing.T) {
	t.Parallel()

	args := map[string]interface{}{
		"name":     "Alice",
		"count":    float64(42),
		"enabled":  true,
		"disabled": false,
	}

	// argString
	if got := argString(args, "name"); got != "Alice" {
		t.Errorf("argString(name) = %q, want %q", got, "Alice")
	}
	if got := argString(args, "missing"); got != "" {
		t.Errorf("argString(missing) = %q, want empty", got)
	}
	if got := argString(args, "count"); got != "" {
		t.Errorf("argString(count) = %q, want empty (wrong type)", got)
	}

	// argFloat
	if got := argFloat(args, "count"); got != 42 {
		t.Errorf("argFloat(count) = %f, want 42", got)
	}
	if got := argFloat(args, "missing"); got != 0 {
		t.Errorf("argFloat(missing) = %f, want 0", got)
	}

	// argBool
	if got := argBool(args, "enabled", false); got != true {
		t.Errorf("argBool(enabled) = %v, want true", got)
	}
	if got := argBool(args, "disabled", true); got != false {
		t.Errorf("argBool(disabled) = %v, want false", got)
	}
	if got := argBool(args, "missing", true); got != true {
		t.Errorf("argBool(missing) = %v, want true (default)", got)
	}

	// argOptionalString
	if v, ok := argOptionalString(args, "name"); !ok || v != "Alice" {
		t.Errorf("argOptionalString(name) = (%q, %v), want (\"Alice\", true)", v, ok)
	}
	if _, ok := argOptionalString(args, "missing"); ok {
		t.Error("argOptionalString(missing) ok = true, want false")
	}
	if _, ok := argOptionalString(args, "count"); ok {
		t.Error("argOptionalString(count) ok = true, want false (wrong type)")
	}
}
