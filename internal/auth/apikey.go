package auth

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"strings"
)

// GenerateAPIKey generates a secure random API key in Claude API format
// Format: sk-<base64-url-encoded-string> (similar to Claude Code API key format)
// Security: 32 bytes of entropy (256 bits)
// @author ygw
func GenerateAPIKey() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("failed to generate random bytes: %w", err)
	}

	// Use URL-safe base64 encoding without padding
	key := base64.RawURLEncoding.EncodeToString(bytes)

	// Format: sk-<random>
	return fmt.Sprintf("sk-%s", key), nil
}

// GetAPIKeyPrefix returns the first 16 characters for logging
// This prevents logging full API keys while still being useful for debugging
func GetAPIKeyPrefix(key string) string {
	if len(key) > 16 {
		return key[:16] + "..."
	}
	return key
}

// IsUserAPIKey checks if an API key has the user key prefix
func IsUserAPIKey(key string) bool {
	return strings.HasPrefix(key, "sk-") || strings.HasPrefix(key, "claude-api_")
}
