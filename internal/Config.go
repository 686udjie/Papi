package internal

import (
	"os"
	"strings"
)

func AuthConfirmedFromEnv() bool {
	if value, ok := os.LookupEnv("PINTEREST_AUTH_CONFIRMED"); ok {
		return isTruthy(value)
	}
	return true
}

func DebugEnabledFromEnv() bool {
	value := os.Getenv("PINTEREST_DEBUG")
	return isTruthy(value)
}

func HasPinterestCredentials() bool {
	return strings.TrimSpace(os.Getenv("PINTEREST_EMAIL")) != "" && strings.TrimSpace(os.Getenv("PINTEREST_PASSWORD")) != ""
}

func isTruthy(value string) bool {
	return value == "true" || value == "TRUE" || value == "1"
}
