//go:build playwright

package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"papi/handlers"
	"papi/storage"
)

type capturePayload struct {
	Cookies       []map[string]any  `json:"cookies"`
	CookiesHeader string            `json:"cookies_header"`
	Headers       map[string]string `json:"headers"`
	UserAgent     string            `json:"user_agent"`
	DataJSON      string            `json:"data_json"`
	SourceURL     string            `json:"source_url"`
	Bookmark      string            `json:"bookmark"`
	CapturedAt    string            `json:"captured_at"`
}

func main() {
	app := buildApp()
	registerRoutes(app)

	go func() {
		if err := runCapture(app); err != nil {
			log.Println("Capture failed:", err)
		} else {
			log.Println("Capture complete; server still running on :8080")
		}
	}()

	log.Println("Running on :8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}

func runCapture(app *handlers.App) error {
	if app == nil || app.Store == nil {
		return nil
	}
	if ok, err := captureNeeded(app); err != nil {
		return err
	} else if !ok {
		return nil
	}

	tmp, err := os.CreateTemp("", "papi-capture-*.json")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	_ = tmp.Close()
	defer func() {
		_ = os.Remove(tmpPath)
	}()

	scriptPath := filepath.Join("playwright", "CaptureHomefeed.js")
	cmd := exec.Command("node", scriptPath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = append(os.Environ(), "CAPTURE_OUTPUT_FILE="+tmpPath)

	log.Println("Please log in to Pinterest in the opened browser window.")
	if err := cmd.Run(); err != nil {
		return err
	}

	raw, err := os.ReadFile(tmpPath)
	if err != nil {
		return err
	}

	var payload capturePayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return err
	}

	cookiesJSON, err := json.Marshal(payload.Cookies)
	if err != nil {
		return err
	}
	headersJSON := ""
	if payload.Headers != nil {
		rawHeaders, err := json.Marshal(payload.Headers)
		if err != nil {
			return err
		}
		headersJSON = string(rawHeaders)
	}

	session := &storage.Session{
		ID:            storage.DefaultSessionID,
		CookiesJSON:   string(cookiesJSON),
		CookiesHeader: payload.CookiesHeader,
		HeadersJSON:   headersJSON,
		UserAgent:     payload.UserAgent,
		DataJSON:      payload.DataJSON,
		SourceURL:     payload.SourceURL,
		Bookmark:      payload.Bookmark,
		UpdatedAt:     time.Now().UTC(),
	}
	return app.Store.UpsertSession(context.Background(), session)
}

func captureNeeded(app *handlers.App) (bool, error) {
	ctx := context.Background()
	session, err := app.Store.GetSession(ctx, storage.DefaultSessionID)
	if err != nil {
		if errors.Is(err, storage.ErrSessionNotFound) {
			return true, nil
		}
		return false, err
	}
	if session == nil {
		return true, nil
	}
	if session.ExpiresAt.Valid && time.Now().After(session.ExpiresAt.Time) {
		return true, nil
	}
	if session.CookiesHeader == "" || session.Bookmark == "" {
		return true, nil
	}
	return false, nil
}
