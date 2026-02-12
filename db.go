package main

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

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
	apiKeyHash := hashAPIKey(apiKey)

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
	apiKeyHash := hashAPIKey(apiKey)

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

func hashAPIKey(apiKey string) string {
	sum := sha256.Sum256([]byte(apiKey))
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
		`, hashAPIKey(r.apiKey), r.id); err != nil {
			return fmt.Errorf("migrate api key for user %d: %w", r.id, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit api key migration: %w", err)
	}
	return nil
}
