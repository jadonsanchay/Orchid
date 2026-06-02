// Package utils provides common utility helpers for Orchid.
package utils

import (
	"bufio"
	"net/url"
	"os"
	"strings"
)

// LoadEnv reads a .env file from the current directory or parent directories if it exists,
// parsing and injecting its key-value pairs into the environment.
// Existing environment variables are NOT overwritten.
func LoadEnv() {
	// Try loading from current directory, and walk up parent directories if not found (useful for nested tests)
	dirPrefix := ""
	for depth := 0; depth < 5; depth++ {
		path := dirPrefix + ".env"
		file, err := os.Open(path)
		if err == nil {
			defer file.Close()
			scanner := bufio.NewScanner(file)
			for scanner.Scan() {
				line := strings.TrimSpace(scanner.Text())
				if line == "" || strings.HasPrefix(line, "#") {
					continue
				}
				parts := strings.SplitN(line, "=", 2)
				if len(parts) != 2 {
					continue
				}
				key := strings.TrimSpace(parts[0])
				val := strings.TrimSpace(parts[1])
				val = strings.Trim(val, `"'`)
				if os.Getenv(key) == "" {
					os.Setenv(key, val)
				}
			}
			return
		}
		dirPrefix = "../" + dirPrefix
	}
}

// EscapeConnectionURI processes a database connection URI (starting with postgres:// or postgresql://)
// and url-encodes the credentials portion (username and password) if they contain special characters.
// This prevents pgx parsing errors when passwords contain symbols like '#', ',', '@', etc.
func EscapeConnectionURI(uri string) string {
	if !strings.HasPrefix(uri, "postgres://") && !strings.HasPrefix(uri, "postgresql://") {
		return uri
	}

	schemeSep := "://"
	schemeIdx := strings.Index(uri, schemeSep)
	if schemeIdx == -1 {
		return uri
	}

	atIdx := strings.LastIndex(uri, "@")
	if atIdx == -1 || atIdx < schemeIdx+len(schemeSep) {
		return uri
	}

	// Extract credentials and host info
	creds := uri[schemeIdx+len(schemeSep) : atIdx]
	parts := strings.SplitN(creds, ":", 2)
	if len(parts) != 2 {
		return uri
	}

	username := parts[0]
	password := parts[1]

	// URL-encode username and password using PathEscape to preserve safety.
	// We escape '@', ':', '#', '?', '/', etc.
	escapedUser := url.PathEscape(username)
	escapedPass := url.PathEscape(password)
	
	rest := uri[atIdx:]
	return uri[:schemeIdx+len(schemeSep)] + escapedUser + ":" + escapedPass + rest
}

