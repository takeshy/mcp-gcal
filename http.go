package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"

	"golang.org/x/oauth2"
)

// HTTPServer serves MCP over HTTP with per-user Google OAuth authentication.
type HTTPServer struct {
	database        *DB
	credentialsFile string
	addr            string
	baseURL         string
	oauthConfig     *oauth2.Config

	// Pending OAuth states (state -> true)
	pendingStates sync.Map
}

// NewHTTPServer creates a new multi-user HTTP MCP server.
func NewHTTPServer(database *DB, credentialsFile, addr, baseURL string) (*HTTPServer, error) {
	// Load OAuth config with email scope for user identification
	config, err := loadOAuthConfig(credentialsFile, oauthScopesWithEmail)
	if err != nil {
		return nil, err
	}

	resolvedBaseURL, err := resolveBaseURL(addr, baseURL)
	if err != nil {
		return nil, err
	}
	config.RedirectURL = resolvedBaseURL + "/auth/callback"

	return &HTTPServer{
		database:        database,
		credentialsFile: credentialsFile,
		addr:            addr,
		baseURL:         resolvedBaseURL,
		oauthConfig:     config,
	}, nil
}

// Run starts the HTTP server.
func (h *HTTPServer) Run(ctx context.Context) error {
	mux := http.NewServeMux()

	// Auth endpoints
	mux.HandleFunc("GET /auth/login", h.handleAuthLogin)
	mux.HandleFunc("GET /auth/callback", h.handleAuthCallback)

	// OAuth discovery (RFC 8414 + RFC 9728)
	mux.HandleFunc("GET /.well-known/oauth-authorization-server", h.handleOAuthMetadata)
	mux.HandleFunc("GET /.well-known/oauth-protected-resource", h.handleProtectedResourceMetadata)
	mux.HandleFunc("GET /.well-known/oauth-protected-resource/{path...}", h.handleProtectedResourceMetadata)

	// OAuth Authorization Server
	mux.HandleFunc("POST /oauth/register", h.handleOAuthRegister)
	mux.HandleFunc("GET /oauth/authorize", h.handleOAuthAuthorize)
	mux.HandleFunc("POST /oauth/token", h.handleOAuthToken)

	// Health check
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	// MCP endpoint (requires Bearer token)
	mux.HandleFunc("POST /mcp", h.handleMCP)

	server := &http.Server{
		Addr:    h.addr,
		Handler: mux,
	}

	go func() {
		<-ctx.Done()
		server.Close()
	}()

	fmt.Fprintf(os.Stderr, "HTTP server listening on %s\n", h.addr)
	fmt.Fprintf(os.Stderr, "Base URL: %s\n", h.baseURL)
	fmt.Fprintf(os.Stderr, "Login URL: %s/auth/login\n", h.baseURL)
	fmt.Fprintf(os.Stderr, "MCP endpoint: %s/mcp\n", h.baseURL)

	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

// handleAuthLogin redirects the user to Google OAuth consent screen.
func (h *HTTPServer) handleAuthLogin(w http.ResponseWriter, r *http.Request) {
	state, err := generateState()
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	h.pendingStates.Store(state, true)

	authURL := h.oauthConfig.AuthCodeURL(state, oauth2.AccessTypeOffline, oauth2.ApprovalForce)
	http.Redirect(w, r, authURL, http.StatusFound)
}

// handleAuthCallback handles the OAuth callback, creates/updates the user, and shows the API key.
func (h *HTTPServer) handleAuthCallback(w http.ResponseWriter, r *http.Request) {
	state := r.URL.Query().Get("state")

	// Check if this is an MCP OAuth flow
	session, err := h.database.GetAuthSessionByState(state)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[ERROR] GetAuthSessionByState: %v\n", err)
	}
	if session != nil {
		h.handleMCPAuthCallback(w, r, session)
		return
	}

	// Legacy flow (pendingStates sync.Map)
	if _, ok := h.pendingStates.LoadAndDelete(state); !ok {
		http.Error(w, "invalid state parameter", http.StatusBadRequest)
		return
	}

	// Check for error
	if errParam := r.URL.Query().Get("error"); errParam != "" {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, `<!DOCTYPE html><html><body><h2>Authentication failed</h2><p>%s</p></body></html>`,
			html.EscapeString(errParam))
		return
	}

	code := r.URL.Query().Get("code")
	if code == "" {
		http.Error(w, "no authorization code", http.StatusBadRequest)
		return
	}

	// Exchange code for token
	tok, err := h.oauthConfig.Exchange(context.Background(), code)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[ERROR] OAuth exchange failed: %v\n", err)
		http.Error(w, "token exchange failed", http.StatusInternalServerError)
		return
	}

	// Fetch user email from Google
	email, err := fetchUserEmail(tok)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[ERROR] Fetch email failed: %v\n", err)
		http.Error(w, "failed to get user email", http.StatusInternalServerError)
		return
	}

	// Create or update user in DB
	apiKey, err := h.database.CreateOrUpdateUser(email, tok)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[ERROR] Create user failed: %v\n", err)
		http.Error(w, "failed to create user", http.StatusInternalServerError)
		return
	}

	fmt.Fprintf(os.Stderr, "[INFO] User authenticated: %s\n", email)

	// Show API key to user
	w.Header().Set("Content-Type", "text/html")
	fmt.Fprintf(w, `<!DOCTYPE html>
<html>
<head><title>Authentication Successful</title>
<style>
body { font-family: -apple-system, BlinkMacSystemFont, sans-serif; max-width: 600px; margin: 40px auto; padding: 20px; }
.key-box { background: #f5f5f5; border: 1px solid #ddd; border-radius: 8px; padding: 16px; margin: 16px 0; word-break: break-all; font-family: monospace; font-size: 14px; }
.copy-btn { padding: 8px 16px; background: #1a73e8; color: white; border: none; border-radius: 6px; cursor: pointer; font-size: 14px; }
.copy-btn:hover { background: #1557b0; }
</style>
</head>
<body>
<h2>Authentication Successful!</h2>
<p>Welcome, <strong>%s</strong></p>
<p>Your API key for MCP requests:</p>
<div class="key-box" id="api-key">%s</div>
<button class="copy-btn" onclick="navigator.clipboard.writeText(document.getElementById('api-key').textContent)">Copy to Clipboard</button>
<h3>Usage</h3>
<p>Include this key as a Bearer token in your MCP requests:</p>
<pre style="background:#f5f5f5;padding:12px;border-radius:6px;overflow-x:auto">Authorization: Bearer %s</pre>
<p>You can close this tab now.</p>
</body>
</html>`, html.EscapeString(email), html.EscapeString(apiKey), html.EscapeString(apiKey))
}

// handleMCP handles MCP JSON-RPC requests with per-user authentication.
// Supports both MCP OAuth tokens and legacy API key Bearer tokens.
func (h *HTTPServer) handleMCP(w http.ResponseWriter, r *http.Request) {
	token := extractBearerToken(r)
	if token == "" {
		setWWWAuthenticate(w, h.baseURL)
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing Authorization header"})
		return
	}

	// 1. Try MCP OAuth token
	userEmail, err := h.database.ValidateMCPAccessToken(token)
	if err == nil && userEmail != "" {
		h.handleMCPRequest(w, r, userEmail)
		return
	}

	// 2. Fallback to legacy API key
	user, err := h.database.GetUserByAPIKey(token)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "database error"})
		return
	}
	if user == nil {
		setWWWAuthenticate(w, h.baseURL)
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid credentials"})
		return
	}
	h.handleMCPRequest(w, r, user.Email)
}

// handleMCPRequest processes a JSON-RPC request for an authenticated user identified by email.
func (h *HTTPServer) handleMCPRequest(w http.ResponseWriter, r *http.Request, userEmail string) {
	// Parse JSON-RPC request
	var req jsonrpcRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONRPC(w, errorResponse(nil, codeParseError, "Parse error", err.Error()))
		return
	}

	if req.JSONRPC != "2.0" {
		writeJSONRPC(w, errorResponse(req.ID, codeInvalidRequest, "Invalid Request", "jsonrpc must be 2.0"))
		return
	}

	// Handle notifications
	if req.ID == nil || string(req.ID) == "null" {
		w.WriteHeader(http.StatusOK)
		return
	}

	var resp *jsonrpcResponse
	switch req.Method {
	case "initialize":
		resp = h.handleInitialize(req.ID)
	case "tools/list":
		resp = h.handleToolsList(req.ID)
	case "tools/call":
		resp = h.handleToolsCall(r.Context(), req.ID, req.Params, userEmail)
	case "resources/list":
		resp = h.handleResourcesList(req.ID)
	case "resources/read":
		resp = h.handleResourcesRead(req.ID, req.Params)
	case "ping":
		resp = successResponse(req.ID, struct{}{})
	default:
		resp = errorResponse(req.ID, codeMethodNotFound, "Method not found", req.Method)
	}

	writeJSONRPC(w, resp)
}

func (h *HTTPServer) handleInitialize(id json.RawMessage) *jsonrpcResponse {
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
	return successResponse(id, result)
}

func (h *HTTPServer) handleToolsList(id json.RawMessage) *jsonrpcResponse {
	all := allTools()
	var tools []mcpTool
	for _, t := range all {
		// In HTTP mode, don't expose the "authenticate" tool (auth is done via /auth/login)
		if t.Name == "authenticate" {
			continue
		}
		if !t.isVisibleToModel() {
			continue
		}
		t.Meta = buildToolMeta(t)
		tools = append(tools, t)
	}
	return successResponse(id, &listToolsResult{Tools: tools})
}

func (h *HTTPServer) handleToolsCall(ctx context.Context, id json.RawMessage, rawParams json.RawMessage, userEmail string) *jsonrpcResponse {
	var params callToolParams
	if err := json.Unmarshal(rawParams, &params); err != nil {
		return errorResponse(id, codeInvalidParams, "Invalid params", err.Error())
	}

	// Build service for this user
	ts, err := getUserTokenSourceByEmail(h.oauthConfig, h.database, userEmail)
	if err != nil {
		return successResponse(id, &callToolResult{
			Content: []content{{Type: "text", Text: fmt.Sprintf("authentication error: %v", err)}},
			IsError: true,
		})
	}

	result, err := dispatchHTTPTool(ctx, ts, params.Name, params.Arguments)
	if err != nil {
		return successResponse(id, &callToolResult{
			Content: []content{{Type: "text", Text: err.Error()}},
			IsError: true,
		})
	}

	jsonBytes, err := json.Marshal(result)
	if err != nil {
		return successResponse(id, &callToolResult{
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
	return successResponse(id, res)
}

func (h *HTTPServer) handleResourcesList(id json.RawMessage) *jsonrpcResponse {
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
	return successResponse(id, &listResourcesResult{Resources: resources})
}

func (h *HTTPServer) handleResourcesRead(id json.RawMessage, rawParams json.RawMessage) *jsonrpcResponse {
	var params readResourceParams
	if err := json.Unmarshal(rawParams, &params); err != nil {
		return errorResponse(id, codeInvalidParams, "Invalid params", err.Error())
	}

	tool, encodedData, err := parseUIResourceURI(params.URI)
	if err != nil {
		return errorResponse(id, codeInvalidParams, "Invalid resource URI", err.Error())
	}

	htmlContent, err := generateUIHTML(*tool, encodedData, "")
	if err != nil {
		return errorResponse(id, codeInternalError, "Failed to generate UI", err.Error())
	}

	return successResponse(id, &readResourceResult{
		Contents: []resourceContent{{
			URI:      params.URI,
			MimeType: "text/html",
			Text:     htmlContent,
		}},
	})
}

// Helper functions

func extractBearerToken(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if auth == "" {
		return ""
	}
	parts := strings.SplitN(auth, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "bearer") {
		return ""
	}
	return parts[1]
}

func generateState() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func writeJSONRPC(w http.ResponseWriter, resp *jsonrpcResponse) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(resp)
}

func resolveBaseURL(addr, baseURL string) (string, error) {
	if baseURL != "" {
		u, err := url.Parse(baseURL)
		if err != nil {
			return "", fmt.Errorf("invalid --base-url %q: %w", baseURL, err)
		}
		if u.Scheme != "http" && u.Scheme != "https" {
			return "", fmt.Errorf("invalid --base-url %q: scheme must be http or https", baseURL)
		}
		if u.Host == "" {
			return "", fmt.Errorf("invalid --base-url %q: host is required", baseURL)
		}
		if u.RawQuery != "" || u.Fragment != "" {
			return "", fmt.Errorf("invalid --base-url %q: query and fragment are not allowed", baseURL)
		}
		return strings.TrimRight(baseURL, "/"), nil
	}

	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "", fmt.Errorf("invalid --addr %q: %w (or specify --base-url)", addr, err)
	}
	switch host {
	case "", "0.0.0.0", "::", "[::]":
		host = "localhost"
	}
	if strings.Contains(host, ":") && !strings.HasPrefix(host, "[") {
		host = "[" + host + "]"
	}
	return fmt.Sprintf("http://%s:%s", host, port), nil
}
