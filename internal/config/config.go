package config

import (
	"encoding/hex"
	"fmt"
	"os"
	"time"
)

type Config struct {
	Address         string
	DatabaseURL     string
	CookieSecure    bool
	SessionLifetime time.Duration
	SessionIdleTime time.Duration
	// EncryptionKey (32 byte) untuk kredensial router/PPPoE; kosong bila
	// APP_ENCRYPTION_KEY tidak disetel — fitur router akan menolak dipakai.
	EncryptionKey []byte
}

func Load() (Config, error) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		return Config{}, fmt.Errorf("DATABASE_URL is required")
	}

	port := os.Getenv("APP_PORT")
	if port == "" {
		port = "8080"
	}

	var encryptionKey []byte
	if raw := os.Getenv("APP_ENCRYPTION_KEY"); raw != "" {
		decoded, err := hex.DecodeString(raw)
		if err != nil || len(decoded) != 32 {
			return Config{}, fmt.Errorf("APP_ENCRYPTION_KEY must be 64 hex characters (32 bytes)")
		}
		encryptionKey = decoded
	}

	return Config{
		Address:         ":" + port,
		DatabaseURL:     databaseURL,
		CookieSecure:    os.Getenv("APP_ENV") != "development",
		SessionLifetime: 12 * time.Hour,
		SessionIdleTime: 30 * time.Minute,
		EncryptionKey:   encryptionKey,
	}, nil
}