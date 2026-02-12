package main

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/calendar/v3"
	"google.golang.org/api/gmail/v1"
)

// OAuth scopes
var oauthScopes = []string{
	calendar.CalendarScope,
	gmail.GmailModifyScope,
}

var oauthScopesWithEmail = []string{
	calendar.CalendarScope,
	gmail.GmailModifyScope,
	"https://www.googleapis.com/auth/userinfo.email",
}

// defaultCredentialsPath returns the default path for OAuth2 credentials.
func defaultCredentialsPath() string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "mcp-gcal", "credentials.json")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "mcp-gcal", "credentials.json")
}

// loadOAuthConfig reads the OAuth2 client credentials JSON file.
func loadOAuthConfig(credentialsFile string, scopes []string) (*oauth2.Config, error) {
	if credentialsFile == "" {
		credentialsFile = defaultCredentialsPath()
	}
	b, err := os.ReadFile(credentialsFile)
	if err != nil {
		return nil, fmt.Errorf("unable to read credentials file %s: %w\nDownload it from Google Cloud Console and place it at %s",
			credentialsFile, err, defaultCredentialsPath())
	}
	config, err := google.ConfigFromJSON(b, scopes...)
	if err != nil {
		return nil, fmt.Errorf("unable to parse credentials: %w", err)
	}
	return config, nil
}

// runOAuthFlow starts the browser-based OAuth2 consent flow and returns the token.
func runOAuthFlow(config *oauth2.Config) (*oauth2.Token, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("failed to start local server: %w", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	config.RedirectURL = fmt.Sprintf("http://localhost:%d/callback", port)
	state, err := generateState()
	if err != nil {
		return nil, fmt.Errorf("generate OAuth state: %w", err)
	}

	codeCh := make(chan string, 1)
	errCh := make(chan error, 1)

	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("state") != state {
			errMsg := "state mismatch"
			fmt.Fprintf(w, "<html><body><h2>Authentication failed</h2><p>%s</p></body></html>", html.EscapeString(errMsg))
			errCh <- fmt.Errorf("auth failed: %s", errMsg)
			return
		}

		code := r.URL.Query().Get("code")
		if code == "" {
			errMsg := r.URL.Query().Get("error")
			if errMsg == "" {
				errMsg = "no authorization code received"
			}
			fmt.Fprintf(w, "<html><body><h2>Authentication failed</h2><p>%s</p></body></html>", html.EscapeString(errMsg))
			errCh <- fmt.Errorf("auth failed: %s", errMsg)
			return
		}
		fmt.Fprintf(w, "<html><body><h2>Authentication successful!</h2><p>You can close this tab.</p></body></html>")
		codeCh <- code
	})

	server := &http.Server{Handler: mux}
	go func() {
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	authURL := config.AuthCodeURL(state, oauth2.AccessTypeOffline, oauth2.ApprovalForce)
	fmt.Fprintf(os.Stderr, "Opening browser for authentication...\n")
	fmt.Fprintf(os.Stderr, "If the browser doesn't open, visit this URL:\n%s\n", authURL)
	openBrowser(authURL)

	var code string
	select {
	case code = <-codeCh:
	case err := <-errCh:
		server.Close()
		return nil, err
	}

	server.Close()

	tok, err := config.Exchange(context.Background(), code)
	if err != nil {
		return nil, fmt.Errorf("unable to exchange auth code for token: %w", err)
	}
	return tok, nil
}

// openBrowser attempts to open a URL in the default browser.
func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	_ = cmd.Start()
}

// getTokenSource loads a token from the single-user DB table and returns a refreshing TokenSource.
func getTokenSource(config *oauth2.Config, database *DB) (oauth2.TokenSource, error) {
	tok, err := database.LoadToken()
	if err != nil {
		return nil, err
	}

	ts := config.TokenSource(context.Background(), tok)
	newTok, err := ts.Token()
	if err != nil {
		return nil, fmt.Errorf("token expired or invalid; run 'mcp-gcal auth' to re-authenticate: %w", err)
	}

	if newTok.AccessToken != tok.AccessToken {
		_ = database.SaveToken(newTok)
	}

	return ts, nil
}

// getUserTokenSource loads a per-user token and returns a refreshing TokenSource.
func getUserTokenSource(config *oauth2.Config, database *DB, apiKey string) (oauth2.TokenSource, string, error) {
	user, err := database.GetUserByAPIKey(apiKey)
	if err != nil {
		return nil, "", err
	}
	if user == nil {
		return nil, "", fmt.Errorf("invalid API key")
	}

	var tok oauth2.Token
	if err := json.Unmarshal([]byte(user.TokenJSON), &tok); err != nil {
		return nil, "", fmt.Errorf("unmarshal user token: %w", err)
	}

	ts := config.TokenSource(context.Background(), &tok)
	newTok, err := ts.Token()
	if err != nil {
		return nil, "", fmt.Errorf("token expired; user must re-authenticate via /auth/login: %w", err)
	}

	// Persist refreshed token
	if newTok.AccessToken != tok.AccessToken {
		_ = database.UpdateUserToken(user.Email, newTok)
	}

	return ts, user.Email, nil
}

// userInfoResponse represents the Google userinfo API response.
type userInfoResponse struct {
	Email string `json:"email"`
}

// fetchUserEmail retrieves the authenticated user's email using the Google userinfo API.
func fetchUserEmail(token *oauth2.Token) (string, error) {
	client := oauth2.NewClient(context.Background(), oauth2.StaticTokenSource(token))
	resp, err := client.Get("https://www.googleapis.com/oauth2/v2/userinfo")
	if err != nil {
		return "", fmt.Errorf("fetch userinfo: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("userinfo API returned status %d", resp.StatusCode)
	}

	var info userInfoResponse
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return "", fmt.Errorf("decode userinfo: %w", err)
	}
	if info.Email == "" {
		return "", fmt.Errorf("no email in userinfo response")
	}
	return info.Email, nil
}
