package config

import (
	"os"
	"time"

	"github.com/joho/godotenv"
)

type Config struct {
	DatabaseURL     string
	WSURL           string
	RESTURL         string
	SyncInterval    time.Duration
	DBWriteInterval time.Duration
}

func Load() (*Config, error) {
	_ = godotenv.Load() // best-effort; .env is optional

	sync, err := time.ParseDuration(getEnv("SYNC_INTERVAL", "30s"))
	if err != nil {
		sync = 30 * time.Second
	}

	write, err := time.ParseDuration(getEnv("DB_WRITE_INTERVAL", "5s"))
	if err != nil {
		write = 5 * time.Second
	}

	return &Config{
		DatabaseURL:     getEnv("DATABASE_URL", "postgres://poly:poly@localhost:5432/polymarket?sslmode=disable"),
		WSURL:           getEnv("WS_URL", "wss://ws-subscriptions-clob.polymarket.com/ws/market"),
		RESTURL:         getEnv("REST_URL", "https://clob.polymarket.com"),
		SyncInterval:    sync,
		DBWriteInterval: write,
	}, nil
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
