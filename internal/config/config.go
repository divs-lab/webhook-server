package config

import (
	"fmt"
	"os"
)

// Config is everything the process reads from the environment.
type Config struct {
	Port        string
	DatabaseURL string
	RabbitMQURL string
	APIKey      string
}

// Load reads a local .env file, then environment variables.
// Only PORT has a default. DATABASE_URL is required once the server starts.
func Load() (Config, error) {
	if err := loadDotEnv(); err != nil {
		return Config{}, err
	}
	cfg := Config{
		Port:        getenv("PORT", "8080"),
		DatabaseURL: os.Getenv("DATABASE_URL"),
		RabbitMQURL: os.Getenv("RABBITMQ_URL"),
		APIKey:      os.Getenv("API_KEY"),
	}
	if cfg.Port == "" {
		return Config{}, fmt.Errorf("PORT must not be empty")
	}
	return cfg, nil
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
