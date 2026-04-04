package internal

import "os"

func AuthConfirmedFromEnv() bool {
	value := os.Getenv("PINTEREST_AUTH_CONFIRMED")
	return value == "true" || value == "TRUE" || value == "1"
}

func DebugEnabledFromEnv() bool {
	value := os.Getenv("PINTEREST_DEBUG")
	return value == "true" || value == "TRUE" || value == "1"
}
