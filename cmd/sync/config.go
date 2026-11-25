package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

const defaultSaveEvery = 10 * time.Second

func resolveDSN() string {
	for _, key := range []string{"DB_URL", "CATSYNC_DB_URL"} {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}

	host := envDefault("POSTGRES_HOST", "localhost")
	port := envDefault("POSTGRES_PORT", "5433")
	user := envDefault("POSTGRES_USER", "binderex")
	password := envDefault("POSTGRES_PASSWORD", "binderex")
	database := envDefault("POSTGRES_DB", "binderex")

	return fmt.Sprintf(
		"postgres://%s:%s@%s:%s/%s?sslmode=disable",
		user, password, host, port, database,
	)
}

func saveInterval() time.Duration {
	value := strings.TrimSpace(os.Getenv("RETRIER_SAVE_INTERVAL"))
	if value == "" {
		return defaultSaveEvery
	}

	if seconds, err := strconv.Atoi(value); err == nil && seconds > 0 {
		return time.Duration(seconds) * time.Second
	}
	if duration, err := time.ParseDuration(value); err == nil && duration > 0 {
		return duration
	}

	return defaultSaveEvery
}

func splitMediums(value string) []string {
	result := make([]string, 0, 2)
	for _, part := range strings.Split(value, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			result = append(result, part)
		}
	}
	return result
}

func containsMedium(values []string, wanted string) bool {
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), wanted) {
			return true
		}
	}
	return false
}

func envDefault(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}
