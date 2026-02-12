package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
)

func defaultDBPath() string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "mcp-gcal", "mcp-gcal.db")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "mcp-gcal", "mcp-gcal.db")
}

func main() {
	// Check for subcommand
	if len(os.Args) > 1 && os.Args[1] == "auth" {
		runAuthCommand()
		return
	}

	// Default: run MCP server
	runServer()
}

func runAuthCommand() {
	fs := flag.NewFlagSet("auth", flag.ExitOnError)
	dbPath := fs.String("db", defaultDBPath(), "SQLite database path")
	credFile := fs.String("credentials-file", "", "Path to OAuth2 credentials JSON file")
	fs.Parse(os.Args[2:])

	if err := os.MkdirAll(filepath.Dir(*dbPath), 0700); err != nil {
		fmt.Fprintf(os.Stderr, "Error creating config directory: %v\n", err)
		os.Exit(1)
	}

	database, err := NewDB(*dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error opening database: %v\n", err)
		os.Exit(1)
	}
	defer database.Close()

	config, err := loadOAuthConfig(*credFile, oauthScopes)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	tok, err := runOAuthFlow(config)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if err := database.SaveToken(tok); err != nil {
		fmt.Fprintf(os.Stderr, "Error saving token: %v\n", err)
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "Authentication successful! Token saved to %s\n", *dbPath)
}

func runServer() {
	dbPath := flag.String("db", defaultDBPath(), "SQLite database path")
	credFile := flag.String("credentials-file", "", "Path to OAuth2 credentials JSON file")
	mode := flag.String("mode", "stdio", "Server mode: stdio (single-user) or http (multi-user)")
	addr := flag.String("addr", ":8080", "HTTP listen address (http mode only)")
	baseURL := flag.String("base-url", "", "Public base URL for OAuth callback (http mode only, default derived from --addr)")
	flag.Parse()

	if err := os.MkdirAll(filepath.Dir(*dbPath), 0700); err != nil {
		fmt.Fprintf(os.Stderr, "Error creating config directory: %v\n", err)
		os.Exit(1)
	}

	database, err := NewDB(*dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error opening database: %v\n", err)
		os.Exit(1)
	}
	defer database.Close()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	switch *mode {
	case "stdio":
		server := NewServer(database, *credFile)
		if err := server.Run(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "Server error: %v\n", err)
			os.Exit(1)
		}

	case "http":
		server, err := NewHTTPServer(database, *credFile, *addr, *baseURL)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error creating HTTP server: %v\n", err)
			os.Exit(1)
		}
		if err := server.Run(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "HTTP server error: %v\n", err)
			os.Exit(1)
		}

	default:
		fmt.Fprintf(os.Stderr, "Unknown mode: %s (use stdio or http)\n", *mode)
		os.Exit(1)
	}
}
