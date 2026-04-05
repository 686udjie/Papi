package internal

import (
	"os"
	"strings"
)

func AuthConfirmedFromEnv() bool {
	if value, ok := os.LookupEnv("PINTEREST_AUTH_CONFIRMED"); ok {
		return isTruthy(value)
	}
	// Convenience: auto-confirm when running via `go run .` in local dev.
	if isGoRunExecutable() {
		return true
	}
	return false
}

func DebugEnabledFromEnv() bool {
	value := os.Getenv("PINTEREST_DEBUG")
	return isTruthy(value)
}

func isTruthy(value string) bool {
	return value == "true" || value == "TRUE" || value == "1"
}

func isGoRunExecutable() bool {
	exe, err := os.Executable()
	if err != nil {
		return false
	}
	// go run builds a temp binary under a go-build cache directory.
	return strings.Contains(exe, "go-build") && strings.Contains(exe, string(os.PathSeparator)+"exe"+string(os.PathSeparator))
}
