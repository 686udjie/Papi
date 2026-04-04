package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"papi/services"
	"papi/storage"
)

type App struct {
	Store         storage.SessionStore
	Client        *http.Client
	AuthConfirmed bool
	Debug         bool
}

func (a *App) Pin(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()

	id := query.Get("id")
	rawURL := query.Get("url")

	result, err := services.FetchPinterest(id, rawURL)
	if err != nil {
		status := http.StatusBadGateway
		if errors.Is(err, services.ErrMissingInput) || errors.Is(err, services.ErrInvalidPinID) {
			status = http.StatusBadRequest
		}
		writeJSON(w, status, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, result)
}

func (a *App) Homefeed(w http.ResponseWriter, r *http.Request) {
	if !a.AuthConfirmed {
		writeJSON(w, http.StatusForbidden, map[string]string{
			"error": "authorization not confirmed; set PINTEREST_AUTH_CONFIRMED=true to enable",
		})
		return
	}

	if a.Store == nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"error": "session store not configured; set DATABASE_URL",
		})
		return
	}

	ctx := r.Context()
	session, err := a.Store.GetSession(ctx, storage.DefaultSessionID)
	if err != nil {
		if errors.Is(err, storage.ErrSessionNotFound) {
			writeJSON(w, http.StatusConflict, map[string]string{
				"error": "session required; run capture",
			})
			return
		}
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}

	if session.ExpiresAt.Valid && time.Now().After(session.ExpiresAt.Time) {
		writeJSON(w, http.StatusConflict, map[string]string{
			"error": "session expired; run capture",
		})
		return
	}

	if session.Bookmark == "" {
		writeJSON(w, http.StatusConflict, map[string]string{
			"error": "bookmark missing; run capture",
		})
		return
	}

	client := a.Client
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}

	body, nextBookmark, status, err := services.FetchHomefeed(
		ctx,
		client,
		session.CookiesHeader,
		session.Bookmark,
		session.HeadersJSON,
		session.UserAgent,
		session.DataJSON,
		session.SourceURL,
	)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}

	if status < 200 || status >= 300 {
		if a.Debug {
			writeJSON(w, http.StatusBadGateway, map[string]string{
				"error":  "upstream returned status " + http.StatusText(status),
				"body":   string(body),
				"status": http.StatusText(status),
			})
			return
		}
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "upstream returned status " + http.StatusText(status)})
		return
	}

	if nextBookmark != "" && nextBookmark != session.Bookmark {
		_ = a.Store.UpdateBookmark(ctx, storage.DefaultSessionID, nextBookmark)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
