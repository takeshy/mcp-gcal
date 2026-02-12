package main

import (
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html"
	"html/template"
	"net/url"
	"strings"
)

//go:embed templates/calendar.html
var calendarTemplate string

// uiResourceURI generates a UI resource URI for a tool.
func uiResourceURI(toolName string) string {
	return fmt.Sprintf("ui://%s/result", toolName)
}

// buildToolMeta creates _meta object for a tool with UI support (used in tools/list).
func buildToolMeta(tool mcpTool) map[string]interface{} {
	if !tool.hasUI() {
		return nil
	}
	uiMeta := map[string]interface{}{
		"resourceUri": uiResourceURI(tool.Name),
	}
	if len(tool.visibility) > 0 {
		uiMeta["visibility"] = tool.visibility
	}
	return map[string]interface{}{
		"ui": uiMeta,
	}
}

// buildResultMeta creates _meta object for a tool call result with output data (used in tools/call).
func buildResultMeta(tool mcpTool, output string) map[string]interface{} {
	if !tool.hasUI() {
		return nil
	}
	encodedOutput := base64.URLEncoding.EncodeToString([]byte(output))
	return map[string]interface{}{
		"ui": map[string]interface{}{
			"resourceUri": fmt.Sprintf("%s?data=%s", uiResourceURI(tool.Name), encodedOutput),
		},
	}
}

// templateData is passed to UI templates.
type templateData struct {
	Output     string
	Lines      []string
	JSON       interface{}
	JSONPretty string
	IsJSON     bool
	SessionID  string
}

// generateUIHTML generates HTML for a tool's UI from its embedded template and encoded output data.
func generateUIHTML(tool mcpTool, encodedData string, sessionID string) (string, error) {
	data, err := base64.URLEncoding.DecodeString(encodedData)
	if err != nil {
		return "", fmt.Errorf("failed to decode data: %w", err)
	}
	output := string(data)

	var tmplContent string
	switch tool.uiTemplate {
	case "templates/calendar.html":
		tmplContent = calendarTemplate
	default:
		return "", fmt.Errorf("unknown template: %s", tool.uiTemplate)
	}

	tmpl, err := template.New("ui").Funcs(template.FuncMap{
		"escape": html.EscapeString,
		"json": func(v interface{}) template.JS {
			b, _ := json.Marshal(v)
			return template.JS(b)
		},
		"jsonPretty": func(v interface{}) template.JS {
			b, _ := json.MarshalIndent(v, "", "  ")
			return template.JS(b)
		},
		"split": strings.Split,
		"join":  strings.Join,
		"slice": func(s []string, start int) []string {
			if start >= len(s) {
				return []string{}
			}
			return s[start:]
		},
		"first": func(s []string) string {
			if len(s) == 0 {
				return ""
			}
			return s[0]
		},
		"contains":  strings.Contains,
		"hasPrefix": strings.HasPrefix,
		"trimSpace": strings.TrimSpace,
	}).Parse(tmplContent)
	if err != nil {
		return "", fmt.Errorf("failed to parse template: %w", err)
	}

	td := templateData{
		Output:    output,
		Lines:     strings.Split(output, "\n"),
		SessionID: sessionID,
	}

	var jsonData interface{}
	if err := json.Unmarshal([]byte(output), &jsonData); err == nil {
		td.JSON = jsonData
		td.IsJSON = true
		if prettyJSON, err := json.MarshalIndent(jsonData, "", "  "); err == nil {
			td.JSONPretty = string(prettyJSON)
		}
	}

	var sb strings.Builder
	if err := tmpl.Execute(&sb, td); err != nil {
		return "", fmt.Errorf("failed to execute template: %w", err)
	}

	return sb.String(), nil
}

// findTool returns a pointer to the tool with the given name, or nil.
func findTool(name string) *mcpTool {
	for _, t := range allTools() {
		if t.Name == name {
			return &t
		}
	}
	return nil
}

// parseUIResourceURI parses a ui:// resource URI and returns the matching tool and encoded data.
func parseUIResourceURI(uri string) (*mcpTool, string, error) {
	u, err := url.Parse(uri)
	if err != nil {
		return nil, "", fmt.Errorf("invalid URI: %w", err)
	}
	if u.Scheme != "ui" {
		return nil, "", fmt.Errorf("unsupported scheme: %s", u.Scheme)
	}
	toolName := u.Host
	tool := findTool(toolName)
	if tool == nil || !tool.hasUI() {
		return nil, "", fmt.Errorf("no UI for tool: %s", toolName)
	}
	data := u.Query().Get("data")
	return tool, data, nil
}
