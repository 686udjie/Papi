package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strings"
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

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type loginResponse struct {
	Status   string `json:"status"`
	Bookmark string `json:"bookmark,omitempty"`
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

func (a *App) Login(w http.ResponseWriter, r *http.Request) {
	if !a.requireStore(w) {
		return
	}

	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{
			"error": "method not allowed",
		})
		return
	}

	var payload loginRequest
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
		return
	}
	if payload.Email == "" || payload.Password == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "email and password are required"})
		return
	}

	ctx := r.Context()
	result, err := services.LoginAndCaptureSession(ctx, payload.Email, payload.Password, a.Store)
	if err != nil {
		switch {
		case errors.Is(err, services.ErrInvalidInput):
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		case errors.Is(err, services.ErrInvalidCredentials):
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": err.Error()})
			return
		case errors.Is(err, services.ErrChallenge):
			writeJSON(w, http.StatusForbidden, map[string]string{"error": err.Error()})
			return
		case errors.Is(err, services.ErrUpstream):
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		case errors.Is(err, services.ErrNetwork):
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		default:
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
	}

	writeJSON(w, http.StatusOK, loginResponse{
		Status:   "ok",
		Bookmark: result.Bookmark,
	})
}

func (a *App) Homefeed(w http.ResponseWriter, r *http.Request) {
	session, err := a.requireSession(w, r)
	if err != nil {
		return
	}

	body, nextBookmark, status, err := services.FetchHomefeed(
		r.Context(),
		a.httpClient(),
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
		_ = a.Store.UpdateBookmark(r.Context(), storage.DefaultSessionID, nextBookmark)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

func (a *App) Search(w http.ResponseWriter, r *http.Request) {
	body, err := a.fetchSearchResponse(w, r)
	if err != nil {
		return
	}

	pinsJSON, err := services.ExtractSearchPinsJSON(string(body))
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(pinsJSON)
}

func (a *App) Board(w http.ResponseWriter, r *http.Request) {
	rawURL := strings.TrimSpace(r.URL.Query().Get("url"))
	if rawURL == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing url"})
		return
	}

	session, err := a.requireSession(w, r)
	if err != nil {
		return
	}

	result, status, err := services.FetchBoard(
		r.Context(),
		a.httpClient(),
		session.CookiesHeader,
		session.HeadersJSON,
		session.UserAgent,
		rawURL,
	)
	if err != nil {
		switch {
		case errors.Is(err, services.ErrMissingBoardURL), errors.Is(err, services.ErrInvalidBoardURL):
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		default:
			if status >= 200 && status < 300 {
				writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
				return
			}
			_ = a.handleUpstreamStatus(w, nil, status)
			return
		}
	}

	writeJSON(w, http.StatusOK, result)
}

func (a *App) fetchSearchResponse(w http.ResponseWriter, r *http.Request) ([]byte, error) {
	query, rs, ok := parseSearchRequest(w, r)
	if !ok {
		return nil, errors.New("invalid search request")
	}
	session, err := a.requireSession(w, r)
	if err != nil {
		return nil, err
	}

	body, status, err := services.FetchSearchPage(
		r.Context(),
		a.httpClient(),
		session.CookiesHeader,
		session.HeadersJSON,
		session.UserAgent,
		query,
		rs,
	)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return nil, err
	}
	if !a.handleUpstreamStatus(w, body, status) {
		return nil, errors.New("upstream returned non-success status")
	}

	return body, nil
}

func parseSearchRequest(w http.ResponseWriter, r *http.Request) (string, string, bool) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if query == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing q"})
		return "", "", false
	}
	rs := strings.TrimSpace(r.URL.Query().Get("rs"))
	if rs == "" {
		rs = "typed"
	}
	return query, rs, true
}

func (a *App) requireSession(w http.ResponseWriter, r *http.Request) (*storage.Session, error) {
	if !a.requireAuthorizedSessionSupport(w) {
		return nil, errors.New("session unavailable")
	}
	session, err := a.ensurePinterestSession(r.Context())
	if err != nil {
		writeSessionError(w, err)
		return nil, err
	}
	return session, nil
}

func (a *App) requireStore(w http.ResponseWriter) bool {
	if a.Store != nil {
		return true
	}
	writeJSON(w, http.StatusInternalServerError, map[string]string{
		"error": "session store not configured; set DATABASE_URL",
	})
	return false
}

func (a *App) requireAuthorizedSessionSupport(w http.ResponseWriter) bool {
	if !a.AuthConfirmed {
		writeJSON(w, http.StatusForbidden, map[string]string{
			"error": "authorization not confirmed; set PINTEREST_AUTH_CONFIRMED=true to enable",
		})
		return false
	}
	return a.requireStore(w)
}

func (a *App) httpClient() *http.Client {
	if a.Client != nil {
		return a.Client
	}
	return &http.Client{Timeout: 15 * time.Second}
}

func (a *App) handleUpstreamStatus(w http.ResponseWriter, body []byte, status int) bool {
	if status >= 200 && status < 300 {
		return true
	}
	if a.Debug {
		writeJSON(w, http.StatusBadGateway, map[string]string{
			"error":  "upstream returned status " + http.StatusText(status),
			"body":   string(body),
			"status": http.StatusText(status),
		})
		return false
	}
	writeJSON(w, http.StatusBadGateway, map[string]string{"error": "upstream returned status " + http.StatusText(status)})
	return false
}

func (a *App) ensurePinterestSession(ctx context.Context) (*storage.Session, error) {
	session, err := a.Store.GetSession(ctx, storage.DefaultSessionID)
	if err == nil && !sessionExpired(session) {
		return session, nil
	}
	if err != nil && !errors.Is(err, storage.ErrSessionNotFound) {
		return nil, err
	}

	email := strings.TrimSpace(os.Getenv("PINTEREST_EMAIL"))
	password := strings.TrimSpace(os.Getenv("PINTEREST_PASSWORD"))
	if email == "" || password == "" {
		if err != nil {
			return nil, storage.ErrSessionNotFound
		}
		return nil, errors.New("session expired and PINTEREST_EMAIL/PINTEREST_PASSWORD are not configured")
	}

	if _, loginErr := services.LoginAndCaptureSession(ctx, email, password, a.Store); loginErr != nil {
		return nil, loginErr
	}

	return a.Store.GetSession(ctx, storage.DefaultSessionID)
}

func sessionExpired(session *storage.Session) bool {
	if session == nil {
		return true
	}
	return session.ExpiresAt.Valid && time.Now().After(session.ExpiresAt.Time)
}

func writeSessionError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, storage.ErrSessionNotFound):
		writeJSON(w, http.StatusConflict, map[string]string{
			"error": "session required; configure PINTEREST_EMAIL and PINTEREST_PASSWORD for auto-login or run /api/login",
		})
	case errors.Is(err, services.ErrInvalidInput):
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
	case errors.Is(err, services.ErrInvalidCredentials):
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": err.Error()})
	case errors.Is(err, services.ErrChallenge):
		writeJSON(w, http.StatusForbidden, map[string]string{"error": err.Error()})
	case errors.Is(err, services.ErrUpstream):
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
	case errors.Is(err, services.ErrNetwork):
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
	default:
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
	}
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
