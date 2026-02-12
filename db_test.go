package main

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

func TestCreateOrUpdateUser_HashesAndRotatesAPIKey(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "test.db")
	d, err := NewDB(dbPath)
	if err != nil {
		t.Fatalf("NewDB() error = %v", err)
	}
	t.Cleanup(func() {
		_ = d.Close()
	})

	token1 := &oauth2.Token{
		AccessToken:  "access-token-1",
		RefreshToken: "refresh-token-1",
		TokenType:    "Bearer",
		Expiry:       time.Now().Add(time.Hour),
	}
	key1, err := d.CreateOrUpdateUser("user@example.com", token1)
	if err != nil {
		t.Fatalf("CreateOrUpdateUser() error = %v", err)
	}
	if !strings.HasPrefix(key1, "gcal_") {
		t.Fatalf("expected returned api key to have gcal_ prefix, got %q", key1)
	}

	var storedHash1 string
	if err := d.db.QueryRow("SELECT api_key FROM users WHERE email = ?", "user@example.com").Scan(&storedHash1); err != nil {
		t.Fatalf("query stored api key hash: %v", err)
	}
	if storedHash1 == key1 {
		t.Fatalf("api key stored in plaintext")
	}
	if strings.HasPrefix(storedHash1, "gcal_") {
		t.Fatalf("stored api key should be hashed, got %q", storedHash1)
	}

	user1, err := d.GetUserByAPIKey(key1)
	if err != nil {
		t.Fatalf("GetUserByAPIKey(first key) error = %v", err)
	}
	if user1 == nil {
		t.Fatalf("GetUserByAPIKey(first key) returned nil user")
	}

	token2 := &oauth2.Token{
		AccessToken:  "access-token-2",
		RefreshToken: "refresh-token-2",
		TokenType:    "Bearer",
		Expiry:       time.Now().Add(2 * time.Hour),
	}
	key2, err := d.CreateOrUpdateUser("user@example.com", token2)
	if err != nil {
		t.Fatalf("CreateOrUpdateUser(second call) error = %v", err)
	}
	if key2 == key1 {
		t.Fatalf("expected API key rotation, but key was reused")
	}

	userOld, err := d.GetUserByAPIKey(key1)
	if err != nil {
		t.Fatalf("GetUserByAPIKey(old key) error = %v", err)
	}
	if userOld != nil {
		t.Fatalf("old API key should be invalid after rotation")
	}

	user2, err := d.GetUserByAPIKey(key2)
	if err != nil {
		t.Fatalf("GetUserByAPIKey(new key) error = %v", err)
	}
	if user2 == nil {
		t.Fatalf("new API key should resolve to a user")
	}

	var userCount int
	if err := d.db.QueryRow("SELECT COUNT(*) FROM users WHERE email = ?", "user@example.com").Scan(&userCount); err != nil {
		t.Fatalf("count users: %v", err)
	}
	if userCount != 1 {
		t.Fatalf("expected one user row, got %d", userCount)
	}
}

func TestMigrateLegacyAPIKeys(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "legacy.db")
	d, err := NewDB(dbPath)
	if err != nil {
		t.Fatalf("NewDB() error = %v", err)
	}
	t.Cleanup(func() {
		_ = d.Close()
	})

	legacyKey := "gcal_legacy_plaintext_key"
	_, err = d.db.Exec(
		`INSERT INTO users (email, api_key, token_json) VALUES (?, ?, ?)`,
		"legacy@example.com",
		legacyKey,
		`{"access_token":"x","token_type":"Bearer"}`,
	)
	if err != nil {
		t.Fatalf("insert legacy user: %v", err)
	}

	if err := d.migrateLegacyAPIKeys(); err != nil {
		t.Fatalf("migrateLegacyAPIKeys() error = %v", err)
	}

	var stored string
	if err := d.db.QueryRow("SELECT api_key FROM users WHERE email = ?", "legacy@example.com").Scan(&stored); err != nil {
		t.Fatalf("query migrated api key: %v", err)
	}
	if stored != hashAPIKey(legacyKey) {
		t.Fatalf("stored hash = %q, want %q", stored, hashAPIKey(legacyKey))
	}

	user, err := d.GetUserByAPIKey(legacyKey)
	if err != nil {
		t.Fatalf("GetUserByAPIKey(legacy key) error = %v", err)
	}
	if user == nil {
		t.Fatalf("legacy key should resolve after migration")
	}
}
