package main

import (
	"context"
	"fmt"
)

// allTools returns all MCP tool definitions with input schemas.
func allTools() []mcpTool {
	return []mcpTool{
		{
			Name:        "authenticate",
			Description: "Authenticate with Google Calendar via OAuth2. Opens a browser for Google login. Must be called before using other tools if not already authenticated.",
			InputSchema: inputSchema{
				Type:       "object",
				Properties: map[string]property{},
			},
		},
		{
			Name:        "list-calendars",
			Description: "List all Google Calendar calendars accessible to the authenticated user.",
			InputSchema: inputSchema{
				Type:       "object",
				Properties: map[string]property{},
			},
		},
		{
			Name:        "list-events",
			Description: "List upcoming events from a Google Calendar.",
			InputSchema: inputSchema{
				Type: "object",
				Properties: map[string]property{
					"calendar_id":   {Type: "string", Description: "Calendar ID (default: primary)"},
					"time_min":      {Type: "string", Description: "Start of time range in RFC3339 format (default: now)"},
					"time_max":      {Type: "string", Description: "End of time range in RFC3339 format (default: 7 days from now)"},
					"max_results":   {Type: "number", Description: "Maximum number of events to return (default: 50)"},
					"single_events": {Type: "boolean", Description: "Whether to expand recurring events (default: true)"},
					"order_by":      {Type: "string", Description: "Sort order: startTime or updated (default: startTime)"},
				},
			},
		},
		{
			Name:        "get-event",
			Description: "Get details of a specific calendar event.",
			InputSchema: inputSchema{
				Type: "object",
				Properties: map[string]property{
					"event_id":    {Type: "string", Description: "Event ID (required)"},
					"calendar_id": {Type: "string", Description: "Calendar ID (default: primary)"},
				},
				Required: []string{"event_id"},
			},
		},
		{
			Name:        "search-events",
			Description: "Search calendar events by text query.",
			InputSchema: inputSchema{
				Type: "object",
				Properties: map[string]property{
					"query":       {Type: "string", Description: "Search query text (required)"},
					"calendar_id": {Type: "string", Description: "Calendar ID (default: primary)"},
					"time_min":    {Type: "string", Description: "Start of time range in RFC3339 format"},
					"time_max":    {Type: "string", Description: "End of time range in RFC3339 format"},
					"max_results": {Type: "number", Description: "Maximum number of events to return (default: 50)"},
				},
				Required: []string{"query"},
			},
		},
		{
			Name:        "create-event",
			Description: "Create a new calendar event. Use RFC3339 for timed events or YYYY-MM-DD for all-day events.",
			InputSchema: inputSchema{
				Type: "object",
				Properties: map[string]property{
					"summary":     {Type: "string", Description: "Event title (required)"},
					"start":       {Type: "string", Description: "Start time in RFC3339 or YYYY-MM-DD (required)"},
					"end":         {Type: "string", Description: "End time in RFC3339 or YYYY-MM-DD (required)"},
					"calendar_id": {Type: "string", Description: "Calendar ID (default: primary)"},
					"description": {Type: "string", Description: "Event description"},
					"location":    {Type: "string", Description: "Event location"},
					"attendees":   {Type: "string", Description: "Comma-separated attendee email addresses"},
					"timezone":    {Type: "string", Description: "Timezone (e.g., America/New_York)"},
				},
				Required: []string{"summary", "start", "end"},
			},
		},
		{
			Name:        "update-event",
			Description: "Update an existing calendar event. Only specified fields are changed.",
			InputSchema: inputSchema{
				Type: "object",
				Properties: map[string]property{
					"event_id":    {Type: "string", Description: "Event ID (required)"},
					"calendar_id": {Type: "string", Description: "Calendar ID (default: primary)"},
					"summary":     {Type: "string", Description: "New event title"},
					"description": {Type: "string", Description: "New description"},
					"location":    {Type: "string", Description: "New location"},
					"start":       {Type: "string", Description: "New start time (RFC3339 or YYYY-MM-DD)"},
					"end":         {Type: "string", Description: "New end time (RFC3339 or YYYY-MM-DD)"},
					"attendees":   {Type: "string", Description: "New comma-separated attendee emails"},
				},
				Required: []string{"event_id"},
			},
		},
		{
			Name:        "delete-event",
			Description: "Delete a calendar event.",
			InputSchema: inputSchema{
				Type: "object",
				Properties: map[string]property{
					"event_id":    {Type: "string", Description: "Event ID (required)"},
					"calendar_id": {Type: "string", Description: "Calendar ID (default: primary)"},
				},
				Required: []string{"event_id"},
			},
		},
		{
			Name:        "respond-to-event",
			Description: "Respond to a calendar event invitation with accepted, declined, or tentative.",
			InputSchema: inputSchema{
				Type: "object",
				Properties: map[string]property{
					"event_id":    {Type: "string", Description: "Event ID (required)"},
					"response":    {Type: "string", Description: "Response: accepted, declined, or tentative (required)"},
					"calendar_id": {Type: "string", Description: "Calendar ID (default: primary)"},
				},
				Required: []string{"event_id", "response"},
			},
		},
		{
			Name:        "show-calendar",
			Description: "Interactive calendar view showing events in a month/week grid with ability to add and delete events",
			InputSchema: inputSchema{
				Type: "object",
				Properties: map[string]property{
					"calendar_id":   {Type: "string", Description: "Calendar ID (default: primary)"},
					"time_min":      {Type: "string", Description: "Start of time range in RFC3339 format (default: now)"},
					"time_max":      {Type: "string", Description: "End of time range in RFC3339 format (default: 7 days from now)"},
					"max_results":   {Type: "number", Description: "Maximum number of events to return (default: 50)"},
					"single_events": {Type: "boolean", Description: "Whether to expand recurring events (default: true)"},
					"order_by":      {Type: "string", Description: "Sort order: startTime or updated (default: startTime)"},
				},
			},
			uiTemplate: "templates/calendar.html",
			visibility: []string{"model", "app"},
		},
		{
			Name:        "gcal-list-events-app",
			Description: "List upcoming events from a Google Calendar (app-only).",
			InputSchema: inputSchema{
				Type: "object",
				Properties: map[string]property{
					"calendar_id":   {Type: "string", Description: "Calendar ID (default: primary)"},
					"time_min":      {Type: "string", Description: "Start of time range in RFC3339 format (default: now)"},
					"time_max":      {Type: "string", Description: "End of time range in RFC3339 format (default: 7 days from now)"},
					"max_results":   {Type: "number", Description: "Maximum number of events to return (default: 50)"},
					"single_events": {Type: "boolean", Description: "Whether to expand recurring events (default: true)"},
					"order_by":      {Type: "string", Description: "Sort order: startTime or updated (default: startTime)"},
				},
			},
			visibility: []string{"app"},
		},
		{
			Name:        "gcal-create-event-app",
			Description: "Create a new calendar event (app-only). Use RFC3339 for timed events or YYYY-MM-DD for all-day events.",
			InputSchema: inputSchema{
				Type: "object",
				Properties: map[string]property{
					"summary":     {Type: "string", Description: "Event title (required)"},
					"start":       {Type: "string", Description: "Start time in RFC3339 or YYYY-MM-DD (required)"},
					"end":         {Type: "string", Description: "End time in RFC3339 or YYYY-MM-DD (required)"},
					"calendar_id": {Type: "string", Description: "Calendar ID (default: primary)"},
					"description": {Type: "string", Description: "Event description"},
					"location":    {Type: "string", Description: "Event location"},
					"attendees":   {Type: "string", Description: "Comma-separated attendee email addresses"},
					"timezone":    {Type: "string", Description: "Timezone (e.g., America/New_York)"},
				},
				Required: []string{"summary", "start", "end"},
			},
			visibility: []string{"app"},
		},
		{
			Name:        "gcal-delete-event-app",
			Description: "Delete a calendar event (app-only).",
			InputSchema: inputSchema{
				Type: "object",
				Properties: map[string]property{
					"event_id":    {Type: "string", Description: "Event ID (required)"},
					"calendar_id": {Type: "string", Description: "Calendar ID (default: primary)"},
				},
				Required: []string{"event_id"},
			},
			visibility: []string{"app"},
		},
		{
			Name:        "gcal-get-event-app",
			Description: "Get details of a specific calendar event (app-only).",
			InputSchema: inputSchema{
				Type: "object",
				Properties: map[string]property{
					"event_id":    {Type: "string", Description: "Event ID (required)"},
					"calendar_id": {Type: "string", Description: "Calendar ID (default: primary)"},
				},
				Required: []string{"event_id"},
			},
			visibility: []string{"app"},
		},
	}
}

// isVisibleToModel returns true if the tool should be visible to model (LLM).
func (t mcpTool) isVisibleToModel() bool {
	if len(t.visibility) == 0 {
		return true
	}
	for _, v := range t.visibility {
		if v == "model" {
			return true
		}
	}
	return false
}

// hasUI returns true if the tool has a UI template.
func (t mcpTool) hasUI() bool {
	return t.uiTemplate != ""
}

// Helper functions for extracting typed values from arguments map

func argString(args map[string]interface{}, key string) string {
	if v, ok := args[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func argFloat(args map[string]interface{}, key string) float64 {
	if v, ok := args[key]; ok {
		if f, ok := v.(float64); ok {
			return f
		}
	}
	return 0
}

func argOptionalString(args map[string]interface{}, key string) (string, bool) {
	v, ok := args[key]
	if !ok {
		return "", false
	}
	s, ok := v.(string)
	if !ok {
		return "", false
	}
	return s, true
}

func argBool(args map[string]interface{}, key string, defaultVal bool) bool {
	if v, ok := args[key]; ok {
		if b, ok := v.(bool); ok {
			return b
		}
	}
	return defaultVal
}

// dispatchCalendarTool routes a calendar tool call to the appropriate CalendarService method.
// This is shared between stdio and HTTP mode.
func dispatchCalendarTool(svc *CalendarService, name string, args map[string]interface{}) (any, error) {
	switch name {
	case "list-calendars":
		return svc.ListCalendars()

	case "list-events", "show-calendar", "gcal-list-events-app":
		return svc.ListEvents(
			argString(args, "calendar_id"),
			argString(args, "time_min"),
			argString(args, "time_max"),
			int64(argFloat(args, "max_results")),
			argBool(args, "single_events", true),
			argString(args, "order_by"),
		)

	case "get-event", "gcal-get-event-app":
		return svc.GetEvent(
			argString(args, "calendar_id"),
			argString(args, "event_id"),
		)

	case "search-events":
		return svc.SearchEvents(
			argString(args, "calendar_id"),
			argString(args, "query"),
			argString(args, "time_min"),
			argString(args, "time_max"),
			int64(argFloat(args, "max_results")),
		)

	case "create-event", "gcal-create-event-app":
		return svc.CreateEvent(
			argString(args, "calendar_id"),
			argString(args, "summary"),
			argString(args, "description"),
			argString(args, "location"),
			argString(args, "start"),
			argString(args, "end"),
			argString(args, "timezone"),
			argString(args, "attendees"),
		)

	case "update-event":
		calID := argString(args, "calendar_id")
		eventID := argString(args, "event_id")
		updates := make(map[string]string)
		for _, key := range []string{"summary", "description", "location", "start", "end", "attendees"} {
			if v, ok := argOptionalString(args, key); ok {
				updates[key] = v
			}
		}
		return svc.UpdateEvent(calID, eventID, updates)

	case "delete-event", "gcal-delete-event-app":
		err := svc.DeleteEvent(
			argString(args, "calendar_id"),
			argString(args, "event_id"),
		)
		if err != nil {
			return nil, err
		}
		return map[string]string{"status": "deleted", "event_id": argString(args, "event_id")}, nil

	case "respond-to-event":
		return svc.RespondToEvent(
			argString(args, "calendar_id"),
			argString(args, "event_id"),
			argString(args, "response"),
		)

	default:
		return nil, fmt.Errorf("unknown tool: %s", name)
	}
}

// dispatchTool routes a tool call for the stdio server (single-user).
func (s *Server) dispatchTool(ctx context.Context, name string, args map[string]interface{}) (any, error) {
	// authenticate is special - doesn't need an existing calendar service
	if name == "authenticate" {
		return s.handleAuthenticate(ctx)
	}

	svc, err := s.ensureCalendarService(ctx)
	if err != nil {
		return nil, fmt.Errorf("calendar service unavailable: %w\nUse the 'authenticate' tool first.", err)
	}

	return dispatchCalendarTool(svc, name, args)
}

// handleAuthenticate performs the OAuth flow and stores the token (stdio mode).
func (s *Server) handleAuthenticate(ctx context.Context) (any, error) {
	config, err := loadOAuthConfig(s.oauthConfig.credentialsFile, oauthScopes)
	if err != nil {
		return nil, err
	}

	tok, err := runOAuthFlow(config)
	if err != nil {
		return nil, fmt.Errorf("OAuth flow failed: %w", err)
	}

	if err := s.database.SaveToken(tok); err != nil {
		return nil, fmt.Errorf("save token: %w", err)
	}

	// Reset cached service so next call uses new token
	s.calendarService = nil

	return map[string]string{"status": "authenticated"}, nil
}
