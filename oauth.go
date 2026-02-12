package main

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"time"

	"golang.org/x/oauth2"
)

const (
	mcpAccessTokenExpiration = 1 * time.Hour
	mcpAuthSessionExpiration = 10 * time.Minute
)

// --- OAuth Discovery Endpoints ---

// handleOAuthMetadata serves RFC 8414 OAuth Authorization Server Metadata.
func (h *HTTPServer) handleOAuthMetadata(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"issuer":                                h.baseURL,
		"authorization_endpoint":                h.baseURL + "/oauth/authorize",
		"token_endpoint":                        h.baseURL + "/oauth/token",
		"registration_endpoint":                 h.baseURL + "/oauth/register",
		"response_types_supported":              []string{"code"},
		"grant_types_supported":                 []string{"authorization_code", "refresh_token"},
		"token_endpoint_auth_methods_supported": []string{"none"},
		"code_challenge_methods_supported":      []string{"S256"},
	})
}

// handleProtectedResourceMetadata serves RFC 9728 Protected Resource Metadata.
func (h *HTTPServer) handleProtectedResourceMetadata(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"resource":              h.baseURL + "/mcp",
		"authorization_servers": []string{h.baseURL},
	})
}

// --- Dynamic Client Registration ---

// handleOAuthRegister implements RFC 7591 Dynamic Client Registration.
func (h *HTTPServer) handleOAuthRegister(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RedirectURIs []string `json:"redirect_uris"`
		ClientName   string   `json:"client_name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}

	if len(req.RedirectURIs) == 0 {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", "redirect_uris is required")
		return
	}

	for _, uri := range req.RedirectURIs {
		u, err := url.Parse(uri)
		if err != nil {
			writeOAuthError(w, http.StatusBadRequest, "invalid_request", fmt.Sprintf("invalid redirect_uri: %s", uri))
			return
		}
		if u.Scheme != "http" && u.Scheme != "https" {
			writeOAuthError(w, http.StatusBadRequest, "invalid_request", fmt.Sprintf("redirect_uri must use http or https scheme: %s", uri))
			return
		}
	}

	clientID, err := h.database.RegisterMCPClient(req.ClientName, req.RedirectURIs)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[ERROR] Register MCP client: %v\n", err)
		writeOAuthError(w, http.StatusInternalServerError, "server_error", "failed to register client")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"client_id":                    clientID,
		"client_name":                  req.ClientName,
		"redirect_uris":               req.RedirectURIs,
		"token_endpoint_auth_method":   "none",
	})
}

// --- Authorization Endpoint ---

// handleOAuthAuthorize implements the OAuth 2.0 authorization endpoint with PKCE.
func (h *HTTPServer) handleOAuthAuthorize(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	responseType := q.Get("response_type")
	clientID := q.Get("client_id")
	redirectURI := q.Get("redirect_uri")
	codeChallenge := q.Get("code_challenge")
	codeChallengeMethod := q.Get("code_challenge_method")
	mcpState := q.Get("state")

	// Validate response_type
	if responseType != "code" {
		writeOAuthError(w, http.StatusBadRequest, "unsupported_response_type", "only 'code' response_type is supported")
		return
	}

	// Validate client_id
	client, err := h.database.GetMCPClient(clientID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[ERROR] Get MCP client: %v\n", err)
		writeOAuthError(w, http.StatusInternalServerError, "server_error", "database error")
		return
	}
	if client == nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", "unknown client_id")
		return
	}

	// Validate redirect_uri
	validRedirect := false
	for _, uri := range client.RedirectURIs {
		if uri == redirectURI {
			validRedirect = true
			break
		}
	}
	if !validRedirect {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", "redirect_uri not registered for this client")
		return
	}

	// Validate PKCE
	if codeChallenge == "" {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", "code_challenge is required")
		return
	}
	if codeChallengeMethod != "S256" {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", "only S256 code_challenge_method is supported")
		return
	}

	// Generate Google OAuth state
	googleState, err := generateSecureToken(16)
	if err != nil {
		writeOAuthError(w, http.StatusInternalServerError, "server_error", "failed to generate state")
		return
	}

	// Save session
	expiresAt := time.Now().UTC().Add(mcpAuthSessionExpiration)
	if err := h.database.CreateAuthSession(googleState, clientID, redirectURI, codeChallenge, "S256", mcpState, expiresAt); err != nil {
		fmt.Fprintf(os.Stderr, "[ERROR] Create auth session: %v\n", err)
		writeOAuthError(w, http.StatusInternalServerError, "server_error", "failed to create session")
		return
	}

	// Redirect to Google OAuth
	authURL := h.oauthConfig.AuthCodeURL(googleState, oauth2.AccessTypeOffline, oauth2.ApprovalForce)
	http.Redirect(w, r, authURL, http.StatusFound)
}

// --- Token Endpoint ---

// handleOAuthToken implements the OAuth 2.0 token endpoint.
func (h *HTTPServer) handleOAuthToken(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", "failed to parse form")
		return
	}

	grantType := r.FormValue("grant_type")
	switch grantType {
	case "authorization_code":
		h.handleAuthorizationCodeGrant(w, r)
	case "refresh_token":
		h.handleRefreshTokenGrant(w, r)
	default:
		writeOAuthError(w, http.StatusBadRequest, "unsupported_grant_type", "only 'authorization_code' and 'refresh_token' grant types are supported")
	}
}

func (h *HTTPServer) handleAuthorizationCodeGrant(w http.ResponseWriter, r *http.Request) {
	code := r.FormValue("code")
	codeVerifier := r.FormValue("code_verifier")
	clientID := r.FormValue("client_id")
	redirectURI := r.FormValue("redirect_uri")

	if code == "" || codeVerifier == "" || clientID == "" {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", "code, code_verifier, and client_id are required")
		return
	}

	// Consume auth code
	session, err := h.database.ConsumeAuthCode(hashToken(code))
	if err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", err.Error())
		return
	}

	// Verify client_id and redirect_uri
	if session.ClientID != clientID {
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "client_id mismatch")
		return
	}
	if session.RedirectURI != redirectURI {
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "redirect_uri mismatch")
		return
	}

	// Verify PKCE
	if !verifyPKCE(codeVerifier, session.CodeChallenge) {
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "PKCE verification failed")
		return
	}

	// Issue tokens
	accessToken, refreshToken, err := h.database.CreateMCPToken(clientID, session.UserEmail)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[ERROR] Create MCP token: %v\n", err)
		writeOAuthError(w, http.StatusInternalServerError, "server_error", "failed to create token")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"access_token":  accessToken,
		"token_type":    "Bearer",
		"expires_in":    int(mcpAccessTokenExpiration.Seconds()),
		"refresh_token": refreshToken,
	})
}

func (h *HTTPServer) handleRefreshTokenGrant(w http.ResponseWriter, r *http.Request) {
	refreshToken := r.FormValue("refresh_token")
	clientID := r.FormValue("client_id")

	if refreshToken == "" || clientID == "" {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", "refresh_token and client_id are required")
		return
	}

	newAccess, newRefresh, err := h.database.RefreshMCPToken(refreshToken, clientID)
	if err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"access_token":  newAccess,
		"token_type":    "Bearer",
		"expires_in":    int(mcpAccessTokenExpiration.Seconds()),
		"refresh_token": newRefresh,
	})
}

// --- MCP Auth Callback Handler ---

// handleMCPAuthCallback is called from handleAuthCallback when the state
// matches an MCP OAuth session (as opposed to the legacy API key flow).
func (h *HTTPServer) handleMCPAuthCallback(w http.ResponseWriter, r *http.Request, session *MCPAuthSession) {
	// Check for Google OAuth error
	if errParam := r.URL.Query().Get("error"); errParam != "" {
		redirectWithError(w, r, session.RedirectURI, "access_denied", errParam, session.MCPState)
		return
	}

	code := r.URL.Query().Get("code")
	if code == "" {
		redirectWithError(w, r, session.RedirectURI, "server_error", "no authorization code from Google", session.MCPState)
		return
	}

	// Exchange Google auth code for token
	tok, err := h.oauthConfig.Exchange(context.Background(), code)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[ERROR] MCP OAuth exchange failed: %v\n", err)
		redirectWithError(w, r, session.RedirectURI, "server_error", "token exchange failed", session.MCPState)
		return
	}

	// Fetch user email
	email, err := fetchUserEmail(tok)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[ERROR] MCP fetch email failed: %v\n", err)
		redirectWithError(w, r, session.RedirectURI, "server_error", "failed to get user email", session.MCPState)
		return
	}

	// Save/update Google token in users table (reuse existing logic, ignore returned API key)
	if _, err := h.database.CreateOrUpdateUser(email, tok); err != nil {
		fmt.Fprintf(os.Stderr, "[ERROR] MCP create user failed: %v\n", err)
		redirectWithError(w, r, session.RedirectURI, "server_error", "failed to create user", session.MCPState)
		return
	}

	fmt.Fprintf(os.Stderr, "[INFO] MCP OAuth user authenticated: %s\n", email)

	// Generate MCP auth code
	mcpAuthCode, err := generateSecureToken(32)
	if err != nil {
		redirectWithError(w, r, session.RedirectURI, "server_error", "failed to generate auth code", session.MCPState)
		return
	}

	// Store auth code hash and email in session
	if err := h.database.SetAuthSessionCode(session.State, hashToken(mcpAuthCode), email); err != nil {
		fmt.Fprintf(os.Stderr, "[ERROR] Set auth session code: %v\n", err)
		redirectWithError(w, r, session.RedirectURI, "server_error", "failed to save auth code", session.MCPState)
		return
	}

	// Redirect to MCP client's redirect_uri with auth code
	u, _ := url.Parse(session.RedirectURI)
	q := u.Query()
	q.Set("code", mcpAuthCode)
	if session.MCPState != "" {
		q.Set("state", session.MCPState)
	}
	u.RawQuery = q.Encode()
	http.Redirect(w, r, u.String(), http.StatusFound)
}

// --- Helper Functions ---

// verifyPKCE verifies the PKCE code_verifier against the stored code_challenge (S256).
func verifyPKCE(codeVerifier, codeChallenge string) bool {
	h := sha256.Sum256([]byte(codeVerifier))
	computed := base64.RawURLEncoding.EncodeToString(h[:])
	return subtle.ConstantTimeCompare([]byte(computed), []byte(codeChallenge)) == 1
}

// writeOAuthError writes an RFC 6749 error response.
func writeOAuthError(w http.ResponseWriter, status int, errorCode, description string) {
	writeJSON(w, status, map[string]string{
		"error":             errorCode,
		"error_description": description,
	})
}

// setWWWAuthenticate sets the WWW-Authenticate header per RFC 6750 with resource metadata link.
func setWWWAuthenticate(w http.ResponseWriter, baseURL string) {
	w.Header().Set("WWW-Authenticate", fmt.Sprintf(
		`Bearer resource_metadata="%s/.well-known/oauth-protected-resource"`,
		baseURL,
	))
}

// redirectWithError redirects to the client's redirect_uri with an error.
func redirectWithError(w http.ResponseWriter, r *http.Request, redirectURI, errorCode, description, state string) {
	u, _ := url.Parse(redirectURI)
	q := u.Query()
	q.Set("error", errorCode)
	q.Set("error_description", description)
	if state != "" {
		q.Set("state", state)
	}
	u.RawQuery = q.Encode()
	http.Redirect(w, r, u.String(), http.StatusFound)
}
