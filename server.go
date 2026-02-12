package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
)

const (
	protocolVersion = "2024-11-05"
	serverName      = "mcp-gcal"
	serverVersion   = "1.0.0"
)

// JSON-RPC 2.0 types

type jsonrpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type jsonrpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// JSON-RPC error codes
const (
	codeParseError     = -32700
	codeInvalidRequest = -32600
	codeMethodNotFound = -32601
	codeInvalidParams  = -32602
	codeInternalError  = -32603
)

// MCP protocol types

type initializeParams struct {
	ProtocolVersion string     `json:"protocolVersion"`
	ClientInfo      clientInfo `json:"clientInfo"`
}

type clientInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type initializeResult struct {
	ProtocolVersion string             `json:"protocolVersion"`
	Capabilities    serverCapabilities `json:"capabilities"`
	ServerInfo      serverInfo         `json:"serverInfo"`
}

type serverCapabilities struct {
	Tools     *toolsCapability     `json:"tools,omitempty"`
	Resources *resourcesCapability `json:"resources,omitempty"`
}

type toolsCapability struct {
	ListChanged bool `json:"listChanged,omitempty"`
}

type resourcesCapability struct {
	Subscribe   bool `json:"subscribe,omitempty"`
	ListChanged bool `json:"listChanged,omitempty"`
}

type serverInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type mcpTool struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	InputSchema inputSchema            `json:"inputSchema"`
	Meta        map[string]interface{} `json:"_meta,omitempty"`
	uiTemplate  string
	visibility  []string
}

type inputSchema struct {
	Type       string              `json:"type"`
	Properties map[string]property `json:"properties,omitempty"`
	Required   []string            `json:"required,omitempty"`
}

type property struct {
	Type        string `json:"type"`
	Description string `json:"description,omitempty"`
}

type listToolsResult struct {
	Tools []mcpTool `json:"tools"`
}

type callToolParams struct {
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments,omitempty"`
}

type callToolResult struct {
	Content []content              `json:"content"`
	IsError bool                   `json:"isError,omitempty"`
	Meta    map[string]interface{} `json:"_meta,omitempty"`
}

type content struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

type resource struct {
	URI         string `json:"uri"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	MimeType    string `json:"mimeType,omitempty"`
}

type listResourcesResult struct {
	Resources []resource `json:"resources"`
}

type readResourceParams struct {
	URI string `json:"uri"`
}

type resourceContent struct {
	URI      string `json:"uri"`
	MimeType string `json:"mimeType,omitempty"`
	Text     string `json:"text,omitempty"`
}

type readResourceResult struct {
	Contents []resourceContent `json:"contents"`
}

// Server is the MCP stdio server.
type Server struct {
	database        *DB
	oauthConfig     *oauthConfigHolder
	calendarService *CalendarService
	initialized     bool
	reader          *bufio.Reader
	writer          io.Writer
}

// oauthConfigHolder lazily holds the OAuth config.
type oauthConfigHolder struct {
	credentialsFile string
}

// NewServer creates a new MCP server.
func NewServer(database *DB, credentialsFile string) *Server {
	return &Server{
		database: database,
		oauthConfig: &oauthConfigHolder{
			credentialsFile: credentialsFile,
		},
		reader: bufio.NewReader(os.Stdin),
		writer: os.Stdout,
	}
}

// Run reads JSON-RPC messages from stdin and writes responses to stdout.
func (s *Server) Run(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		line, err := s.reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return fmt.Errorf("read: %w", err)
		}

		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		resp := s.handleMessage(ctx, []byte(line))
		if resp != nil {
			if err := s.writeResponse(resp); err != nil {
				return fmt.Errorf("write response: %w", err)
			}
		}
	}
}

func (s *Server) handleMessage(ctx context.Context, data []byte) *jsonrpcResponse {
	var req jsonrpcRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return errorResponse(nil, codeParseError, "Parse error", err.Error())
	}

	if req.JSONRPC != "2.0" {
		return errorResponse(req.ID, codeInvalidRequest, "Invalid Request", "jsonrpc must be 2.0")
	}

	// Notifications (no id) don't get responses
	if req.ID == nil || string(req.ID) == "null" {
		s.handleNotification(&req)
		return nil
	}

	return s.handleRequest(ctx, &req)
}

func (s *Server) handleNotification(req *jsonrpcRequest) {
	switch req.Method {
	case "notifications/initialized":
		s.initialized = true
	case "notifications/cancelled":
		// no-op
	default:
		fmt.Fprintf(os.Stderr, "unknown notification: %s\n", req.Method)
	}
}

func (s *Server) handleRequest(ctx context.Context, req *jsonrpcRequest) *jsonrpcResponse {
	switch req.Method {
	case "initialize":
		return s.handleInitialize(req)
	case "tools/list":
		return s.handleToolsList(req)
	case "tools/call":
		return s.handleToolsCall(ctx, req)
	case "resources/list":
		return s.handleResourcesList(req)
	case "resources/read":
		return s.handleResourcesRead(req)
	case "ping":
		return successResponse(req.ID, struct{}{})
	default:
		return errorResponse(req.ID, codeMethodNotFound, "Method not found", req.Method)
	}
}

func (s *Server) handleInitialize(req *jsonrpcRequest) *jsonrpcResponse {
	result := &initializeResult{
		ProtocolVersion: protocolVersion,
		Capabilities: serverCapabilities{
			Tools:     &toolsCapability{ListChanged: false},
			Resources: &resourcesCapability{},
		},
		ServerInfo: serverInfo{
			Name:    serverName,
			Version: serverVersion,
		},
	}
	return successResponse(req.ID, result)
}

func (s *Server) handleToolsList(req *jsonrpcRequest) *jsonrpcResponse {
	all := allTools()
	var tools []mcpTool
	for _, t := range all {
		if t.isVisibleToModel() {
			t.Meta = buildToolMeta(t)
			tools = append(tools, t)
		}
	}
	return successResponse(req.ID, &listToolsResult{Tools: tools})
}

func (s *Server) handleToolsCall(ctx context.Context, req *jsonrpcRequest) *jsonrpcResponse {
	var params callToolParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return errorResponse(req.ID, codeInvalidParams, "Invalid params", err.Error())
	}

	result, err := s.dispatchTool(ctx, params.Name, params.Arguments)
	if err != nil {
		return successResponse(req.ID, &callToolResult{
			Content: []content{{Type: "text", Text: err.Error()}},
			IsError: true,
		})
	}

	// Marshal result to JSON text
	jsonBytes, err := json.Marshal(result)
	if err != nil {
		return successResponse(req.ID, &callToolResult{
			Content: []content{{Type: "text", Text: fmt.Sprintf("marshal error: %v", err)}},
			IsError: true,
		})
	}

	res := &callToolResult{
		Content: []content{{Type: "text", Text: string(jsonBytes)}},
	}
	if tool := findTool(params.Name); tool != nil && tool.hasUI() {
		res.Meta = buildResultMeta(*tool, string(jsonBytes))
	}
	return successResponse(req.ID, res)
}

func (s *Server) handleResourcesList(req *jsonrpcRequest) *jsonrpcResponse {
	resources := []resource{}
	for _, t := range allTools() {
		if t.hasUI() {
			resources = append(resources, resource{
				URI:         uiResourceURI(t.Name),
				Name:        t.Name + " UI",
				Description: "Interactive UI for " + t.Name + " tool",
				MimeType:    "text/html",
			})
		}
	}
	return successResponse(req.ID, &listResourcesResult{Resources: resources})
}

func (s *Server) handleResourcesRead(req *jsonrpcRequest) *jsonrpcResponse {
	var params readResourceParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return errorResponse(req.ID, codeInvalidParams, "Invalid params", err.Error())
	}

	tool, encodedData, err := parseUIResourceURI(params.URI)
	if err != nil {
		return errorResponse(req.ID, codeInvalidParams, "Invalid resource URI", err.Error())
	}

	htmlContent, err := generateUIHTML(*tool, encodedData, "")
	if err != nil {
		return errorResponse(req.ID, codeInternalError, "Failed to generate UI", err.Error())
	}

	return successResponse(req.ID, &readResourceResult{
		Contents: []resourceContent{{
			URI:      params.URI,
			MimeType: "text/html",
			Text:     htmlContent,
		}},
	})
}

func (s *Server) writeResponse(resp *jsonrpcResponse) error {
	data, err := json.Marshal(resp)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(s.writer, "%s\n", data)
	return err
}

// ensureCalendarService lazily initializes the CalendarService.
func (s *Server) ensureCalendarService(ctx context.Context) (*CalendarService, error) {
	if s.calendarService != nil {
		return s.calendarService, nil
	}

	config, err := loadOAuthConfig(s.oauthConfig.credentialsFile, oauthScopes)
	if err != nil {
		return nil, err
	}

	ts, err := getTokenSource(config, s.database)
	if err != nil {
		return nil, err
	}

	svc, err := NewCalendarService(ctx, ts)
	if err != nil {
		return nil, err
	}

	s.calendarService = svc
	return svc, nil
}

func successResponse(id json.RawMessage, result any) *jsonrpcResponse {
	return &jsonrpcResponse{
		JSONRPC: "2.0",
		ID:      id,
		Result:  result,
	}
}

func errorResponse(id json.RawMessage, code int, message string, data any) *jsonrpcResponse {
	return &jsonrpcResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error: &rpcError{
			Code:    code,
			Message: message,
			Data:    data,
		},
	}
}
