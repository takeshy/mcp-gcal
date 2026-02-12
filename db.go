package main

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"golang.org/x/oauth2"
)

// DB wraps a SQLite database for token and user storage.
type DB struct {
	db *sql.DB
}

// User represents an authenticated user.
type User struct {
	ID         int64
	Email      string
	APIKeyHash string
	TokenJSON  string
}

// MCPOAuthClient represents a dynamically registered MCP OAuth client.
type MCPOAuthClient struct {
	ID               int64
	ClientID         string
	ClientSecretHash *string
	ClientName       string
	RedirectURIs     []string
	CreatedAt        string
}

// MCPAuthSession represents an in-flight MCP OAuth authorization session.
type MCPAuthSession struct {
	ID                  int64
	State               string
	ClientID            string
	RedirectURI         string
	CodeChallenge       string
	CodeChallengeMethod string
	MCPState            string
	AuthCodeHash        string
	UserEmail           string
	ExpiresAt           string
	Used                bool
}

// NewDB opens (or creates) a SQLite database at path and runs migrations.
func NewDB(path string) (*DB, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, fmt.Errorf("set WAL mode: %w", err)
	}

	// Single-user token table (for stdio mode)
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS oauth_tokens (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			token_json TEXT NOT NULL,
			updated_at TEXT DEFAULT (datetime('now'))
		)
	`); err != nil {
		db.Close()
		return nil, fmt.Errorf("create oauth_tokens table: %w", err)
	}

	// Multi-user table (for HTTP mode)
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			email TEXT UNIQUE NOT NULL,
			api_key TEXT UNIQUE NOT NULL,
			token_json TEXT NOT NULL,
			created_at TEXT DEFAULT (datetime('now')),
			updated_at TEXT DEFAULT (datetime('now'))
		)
	`); err != nil {
		db.Close()
		return nil, fmt.Errorf("create users table: %w", err)
	}

	// MCP OAuth clients (Dynamic Client Registration)
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS mcp_oauth_clients (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			client_id TEXT UNIQUE NOT NULL,
			client_secret_hash TEXT,
			client_name TEXT,
			redirect_uris TEXT NOT NULL,
			created_at TEXT DEFAULT (datetime('now'))
		)
	`); err != nil {
		db.Close()
		return nil, fmt.Errorf("create mcp_oauth_clients table: %w", err)
	}

	// MCP OAuth authorization sessions
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS mcp_oauth_sessions (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			state TEXT UNIQUE NOT NULL,
			client_id TEXT NOT NULL,
			redirect_uri TEXT NOT NULL,
			code_challenge TEXT NOT NULL,
			code_challenge_method TEXT NOT NULL DEFAULT 'S256',
			mcp_state TEXT,
			auth_code_hash TEXT UNIQUE,
			user_email TEXT,
			expires_at TEXT NOT NULL,
			used INTEGER NOT NULL DEFAULT 0,
			created_at TEXT DEFAULT (datetime('now'))
		)
	`); err != nil {
		db.Close()
		return nil, fmt.Errorf("create mcp_oauth_sessions table: %w", err)
	}

	// MCP OAuth access/refresh tokens
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS mcp_oauth_tokens (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			client_id TEXT NOT NULL,
			user_email TEXT NOT NULL,
			access_token_hash TEXT UNIQUE NOT NULL,
			refresh_token_hash TEXT UNIQUE NOT NULL,
			expires_at TEXT NOT NULL,
			created_at TEXT DEFAULT (datetime('now'))
		)
	`); err != nil {
		db.Close()
		return nil, fmt.Errorf("create mcp_oauth_tokens table: %w", err)
	}

	d := &DB{db: db}
	if err := d.migrateLegacyAPIKeys(); err != nil {
		db.Close()
		return nil, err
	}

	return d, nil
}

// --- Single-user methods (stdio mode) ---

// SaveToken stores an OAuth2 token in the single-user table.
func (d *DB) SaveToken(token *oauth2.Token) error {
	data, err := json.Marshal(token)
	if err != nil {
		return fmt.Errorf("marshal token: %w", err)
	}
	_, err = d.db.Exec(`
		INSERT OR REPLACE INTO oauth_tokens (id, token_json, updated_at)
		VALUES (1, ?, datetime('now'))
	`, string(data))
	return err
}

// LoadToken retrieves the stored single-user OAuth2 token.
func (d *DB) LoadToken() (*oauth2.Token, error) {
	var tokenJSON string
	err := d.db.QueryRow("SELECT token_json FROM oauth_tokens WHERE id = 1").Scan(&tokenJSON)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("no token stored; run 'mcp-gcal auth' first")
		}
		return nil, err
	}
	var token oauth2.Token
	if err := json.Unmarshal([]byte(tokenJSON), &token); err != nil {
		return nil, fmt.Errorf("unmarshal token: %w", err)
	}
	return &token, nil
}

// --- Multi-user methods (HTTP mode) ---

// CreateOrUpdateUser creates a new user or updates an existing one.
// Returns the API key for the user.
func (d *DB) CreateOrUpdateUser(email string, token *oauth2.Token) (string, error) {
	tokenData, err := json.Marshal(token)
	if err != nil {
		return "", fmt.Errorf("marshal token: %w", err)
	}

	apiKey, err := generateAPIKey()
	if err != nil {
		return "", fmt.Errorf("generate API key: %w", err)
	}
	apiKeyHash := hashToken(apiKey)

	// Check if user exists
	var userID int64
	err = d.db.QueryRow("SELECT id FROM users WHERE email = ?", email).Scan(&userID)
	switch err {
	case nil:
		// User exists: rotate API key and update token.
		_, err = d.db.Exec(`
			UPDATE users SET api_key = ?, token_json = ?, updated_at = datetime('now') WHERE id = ?
		`, apiKeyHash, string(tokenData), userID)
		if err != nil {
			return "", fmt.Errorf("update user: %w", err)
		}
		return apiKey, nil
	case sql.ErrNoRows:
		// New user
	default:
		return "", fmt.Errorf("lookup user by email: %w", err)
	}

	_, err = d.db.Exec(`
		INSERT INTO users (email, api_key, token_json) VALUES (?, ?, ?)
	`, email, apiKeyHash, string(tokenData))
	if err != nil {
		return "", fmt.Errorf("insert user: %w", err)
	}
	return apiKey, nil
}

// GetUserByAPIKey looks up a user by their API key.
func (d *DB) GetUserByAPIKey(apiKey string) (*User, error) {
	apiKeyHash := hashToken(apiKey)

	var u User
	err := d.db.QueryRow(
		"SELECT id, email, api_key, token_json FROM users WHERE api_key = ?", apiKeyHash,
	).Scan(&u.ID, &u.Email, &u.APIKeyHash, &u.TokenJSON)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &u, nil
}

// GetUserToken parses the stored token for a user.
func (d *DB) GetUserToken(apiKey string) (*oauth2.Token, error) {
	u, err := d.GetUserByAPIKey(apiKey)
	if err != nil {
		return nil, err
	}
	if u == nil {
		return nil, fmt.Errorf("user not found")
	}
	var token oauth2.Token
	if err := json.Unmarshal([]byte(u.TokenJSON), &token); err != nil {
		return nil, fmt.Errorf("unmarshal token: %w", err)
	}
	return &token, nil
}

// UpdateUserToken saves a refreshed token for a user identified by email.
func (d *DB) UpdateUserToken(email string, token *oauth2.Token) error {
	data, err := json.Marshal(token)
	if err != nil {
		return fmt.Errorf("marshal token: %w", err)
	}
	_, err = d.db.Exec(`
		UPDATE users SET token_json = ?, updated_at = datetime('now') WHERE email = ?
	`, string(data), email)
	return err
}

// --- MCP OAuth client methods ---

// RegisterMCPClient registers a new MCP OAuth client with a generated UUID.
func (d *DB) RegisterMCPClient(clientName string, redirectURIs []string) (string, error) {
	clientID, err := generateSecureToken(16)
	if err != nil {
		return "", fmt.Errorf("generate client id: %w", err)
	}

	urisJSON, err := json.Marshal(redirectURIs)
	if err != nil {
		return "", fmt.Errorf("marshal redirect_uris: %w", err)
	}

	_, err = d.db.Exec(`
		INSERT INTO mcp_oauth_clients (client_id, client_name, redirect_uris)
		VALUES (?, ?, ?)
	`, clientID, clientName, string(urisJSON))
	if err != nil {
		return "", fmt.Errorf("insert mcp oauth client: %w", err)
	}
	return clientID, nil
}

// GetMCPClient looks up an MCP OAuth client by client_id.
func (d *DB) GetMCPClient(clientID string) (*MCPOAuthClient, error) {
	var c MCPOAuthClient
	var urisJSON string
	err := d.db.QueryRow(
		"SELECT id, client_id, client_secret_hash, client_name, redirect_uris, created_at FROM mcp_oauth_clients WHERE client_id = ?",
		clientID,
	).Scan(&c.ID, &c.ClientID, &c.ClientSecretHash, &c.ClientName, &urisJSON, &c.CreatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	if err := json.Unmarshal([]byte(urisJSON), &c.RedirectURIs); err != nil {
		return nil, fmt.Errorf("unmarshal redirect_uris: %w", err)
	}
	return &c, nil
}

// --- MCP OAuth session methods ---

// CreateAuthSession creates a new MCP OAuth authorization session.
func (d *DB) CreateAuthSession(state, clientID, redirectURI, codeChallenge, codeChallengeMethod, mcpState string, expiresAt time.Time) error {
	_, err := d.db.Exec(`
		INSERT INTO mcp_oauth_sessions (state, client_id, redirect_uri, code_challenge, code_challenge_method, mcp_state, expires_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, state, clientID, redirectURI, codeChallenge, codeChallengeMethod, mcpState, expiresAt.UTC().Format(time.RFC3339))
	return err
}

// GetAuthSessionByState looks up an MCP OAuth session by the Google OAuth state parameter.
func (d *DB) GetAuthSessionByState(state string) (*MCPAuthSession, error) {
	var s MCPAuthSession
	var used int
	var authCodeHash, userEmail, mcpState sql.NullString
	err := d.db.QueryRow(
		"SELECT id, state, client_id, redirect_uri, code_challenge, code_challenge_method, mcp_state, auth_code_hash, user_email, expires_at, used FROM mcp_oauth_sessions WHERE state = ?",
		state,
	).Scan(&s.ID, &s.State, &s.ClientID, &s.RedirectURI, &s.CodeChallenge, &s.CodeChallengeMethod, &mcpState, &authCodeHash, &userEmail, &s.ExpiresAt, &used)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	s.MCPState = mcpState.String
	s.AuthCodeHash = authCodeHash.String
	s.UserEmail = userEmail.String
	s.Used = used != 0
	return &s, nil
}

// SetAuthSessionCode sets the auth code hash and user email on an existing session.
func (d *DB) SetAuthSessionCode(state, authCodeHash, userEmail string) error {
	result, err := d.db.Exec(`
		UPDATE mcp_oauth_sessions SET auth_code_hash = ?, user_email = ? WHERE state = ?
	`, authCodeHash, userEmail, state)
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return fmt.Errorf("session not found for state")
	}
	return nil
}

// ConsumeAuthCode atomically marks a session's auth code as used and returns it.
// Returns an error if the code is invalid, already used, or expired.
func (d *DB) ConsumeAuthCode(authCodeHash string) (*MCPAuthSession, error) {
	now := time.Now().UTC().Format(time.RFC3339)

	// Atomic: UPDATE only if unused and not expired
	result, err := d.db.Exec(
		"UPDATE mcp_oauth_sessions SET used = 1 WHERE auth_code_hash = ? AND used = 0 AND expires_at > ?",
		authCodeHash, now,
	)
	if err != nil {
		return nil, fmt.Errorf("consume auth code: %w", err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		// Distinguish between invalid, used, and expired for error message
		var used int
		var expiresAt string
		err := d.db.QueryRow(
			"SELECT used, expires_at FROM mcp_oauth_sessions WHERE auth_code_hash = ?", authCodeHash,
		).Scan(&used, &expiresAt)
		if err != nil {
			return nil, fmt.Errorf("invalid authorization code")
		}
		if used != 0 {
			return nil, fmt.Errorf("authorization code already used")
		}
		return nil, fmt.Errorf("authorization code expired")
	}

	// Fetch the consumed session
	var s MCPAuthSession
	var mcpState, userEmail sql.NullString
	err = d.db.QueryRow(
		"SELECT id, state, client_id, redirect_uri, code_challenge, code_challenge_method, mcp_state, auth_code_hash, user_email, expires_at FROM mcp_oauth_sessions WHERE auth_code_hash = ?",
		authCodeHash,
	).Scan(&s.ID, &s.State, &s.ClientID, &s.RedirectURI, &s.CodeChallenge, &s.CodeChallengeMethod, &mcpState, &s.AuthCodeHash, &userEmail, &s.ExpiresAt)
	if err != nil {
		return nil, fmt.Errorf("fetch consumed session: %w", err)
	}
	s.MCPState = mcpState.String
	s.UserEmail = userEmail.String
	s.Used = true

	return &s, nil
}

// --- MCP OAuth token methods ---

// CreateMCPToken generates and stores a new MCP access/refresh token pair.
func (d *DB) CreateMCPToken(clientID, userEmail string) (accessToken, refreshToken string, err error) {
	accessToken, err = generateSecureToken(32)
	if err != nil {
		return "", "", fmt.Errorf("generate access token: %w", err)
	}
	refreshToken, err = generateSecureToken(32)
	if err != nil {
		return "", "", fmt.Errorf("generate refresh token: %w", err)
	}

	expiresAt := time.Now().UTC().Add(mcpAccessTokenExpiration).Format(time.RFC3339)
	_, err = d.db.Exec(`
		INSERT INTO mcp_oauth_tokens (client_id, user_email, access_token_hash, refresh_token_hash, expires_at)
		VALUES (?, ?, ?, ?, ?)
	`, clientID, userEmail, hashToken(accessToken), hashToken(refreshToken), expiresAt)
	if err != nil {
		return "", "", fmt.Errorf("insert mcp token: %w", err)
	}
	return accessToken, refreshToken, nil
}

// ValidateMCPAccessToken checks a bearer token and returns the associated user email.
func (d *DB) ValidateMCPAccessToken(token string) (string, error) {
	h := hashToken(token)
	var userEmail string
	var expiresAt string
	err := d.db.QueryRow(
		"SELECT user_email, expires_at FROM mcp_oauth_tokens WHERE access_token_hash = ?", h,
	).Scan(&userEmail, &expiresAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", fmt.Errorf("invalid access token")
		}
		return "", err
	}

	exp, err := time.Parse(time.RFC3339, expiresAt)
	if err != nil {
		return "", fmt.Errorf("parse expires_at: %w", err)
	}
	if time.Now().UTC().After(exp) {
		return "", fmt.Errorf("access token expired")
	}

	return userEmail, nil
}

// RefreshMCPToken exchanges a refresh token for a new access/refresh token pair.
func (d *DB) RefreshMCPToken(refreshToken, clientID string) (string, string, error) {
	h := hashToken(refreshToken)
	var tokenID int64
	var userEmail, storedClientID string
	err := d.db.QueryRow(
		"SELECT id, client_id, user_email FROM mcp_oauth_tokens WHERE refresh_token_hash = ?", h,
	).Scan(&tokenID, &storedClientID, &userEmail)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", "", fmt.Errorf("invalid refresh token")
		}
		return "", "", err
	}

	if storedClientID != clientID {
		return "", "", fmt.Errorf("client_id mismatch")
	}

	// Delete old token
	if _, err := d.db.Exec("DELETE FROM mcp_oauth_tokens WHERE id = ?", tokenID); err != nil {
		return "", "", fmt.Errorf("delete old token: %w", err)
	}

	// Issue new pair
	return d.CreateMCPToken(clientID, userEmail)
}

// CleanupExpiredMCPData removes expired sessions and stale tokens.
// Sessions are deleted as soon as they expire.
// Tokens are kept for 7 days past access token expiry so that refresh tokens
// remain usable even after the access token has expired.
func (d *DB) CleanupExpiredMCPData() error {
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := d.db.Exec("DELETE FROM mcp_oauth_sessions WHERE expires_at < ?", now); err != nil {
		return fmt.Errorf("cleanup sessions: %w", err)
	}
	stale := time.Now().UTC().Add(-7 * 24 * time.Hour).Format(time.RFC3339)
	if _, err := d.db.Exec("DELETE FROM mcp_oauth_tokens WHERE expires_at < ?", stale); err != nil {
		return fmt.Errorf("cleanup tokens: %w", err)
	}
	return nil
}

// --- User lookup by email ---

// GetUserByEmail looks up a user by their email address.
func (d *DB) GetUserByEmail(email string) (*User, error) {
	var u User
	err := d.db.QueryRow(
		"SELECT id, email, api_key, token_json FROM users WHERE email = ?", email,
	).Scan(&u.ID, &u.Email, &u.APIKeyHash, &u.TokenJSON)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &u, nil
}

// Close closes the database.
func (d *DB) Close() error {
	return d.db.Close()
}

func generateAPIKey() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "gcal_" + hex.EncodeToString(b), nil
}

// generateSecureToken generates a cryptographically random token of the given byte length.
func generateSecureToken(length int) (string, error) {
	b := make([]byte, length)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// hashToken returns the hex-encoded SHA256 hash of a token.
func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// migrateLegacyAPIKeys hashes previously stored plaintext API keys.
func (d *DB) migrateLegacyAPIKeys() error {
	rows, err := d.db.Query("SELECT id, api_key FROM users")
	if err != nil {
		return fmt.Errorf("list users for api key migration: %w", err)
	}
	defer rows.Close()

	type row struct {
		id     int64
		apiKey string
	}

	var legacyRows []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.apiKey); err != nil {
			return fmt.Errorf("scan user api key: %w", err)
		}
		if strings.HasPrefix(r.apiKey, "gcal_") {
			legacyRows = append(legacyRows, r)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate users for api key migration: %w", err)
	}
	if len(legacyRows) == 0 {
		return nil
	}

	tx, err := d.db.Begin()
	if err != nil {
		return fmt.Errorf("begin api key migration tx: %w", err)
	}
	defer tx.Rollback()

	for _, r := range legacyRows {
		if _, err := tx.Exec(`
			UPDATE users SET api_key = ?, updated_at = datetime('now') WHERE id = ?
		`, hashToken(r.apiKey), r.id); err != nil {
			return fmt.Errorf("migrate api key for user %d: %w", r.id, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit api key migration: %w", err)
	}
	return nil
}
