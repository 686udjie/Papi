package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strings"
	"time"

	"papi/parsers"
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

func (a *App) User(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	profileURL := query.Get("url")

	// Optional session for better data (counts, etc)
	var cookiesHeader string
	client := a.httpClient()
	session, _ := a.Store.GetSession(r.Context(), storage.DefaultSessionID)
	if session != nil && !storage.SessionExpired(session) {
		cookiesHeader = session.CookiesHeader
	}

	result, err := services.FetchUser(r.Context(), client, cookiesHeader, profileURL)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
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
	query, rs, filter, ok := parseSearchRequest(w, r)
	if !ok {
		return
	}

	session, err := a.requireSession(w, r)
	if err != nil {
		return
	}

	// 1. Generate query variants
	variants := services.GenerateQueryVariants(query)
	// 2. Rotate (randomly pick one for diversity)
	selectedQuery := variants[time.Now().UnixNano()%int64(len(variants))]

	// 3. Get bookmark for the selected variant
	bookmark, _ := a.Store.GetSearchBookmark(r.Context(), selectedQuery)

	// 4. Fetch results
	body, nextBookmark, status, err := services.FetchSearchResource(
		r.Context(),
		a.httpClient(),
		session.CookiesHeader,
		session.HeadersJSON,
		session.UserAgent,
		selectedQuery,
		bookmark,
		filter,
		rs,
	)

	isHTML := false
	if err != nil || status == http.StatusNotFound || status == http.StatusForbidden {
		// Fallback to HTML page
		body, status, err = services.FetchSearchPage(
			r.Context(),
			a.httpClient(),
			session.CookiesHeader,
			session.HeadersJSON,
			session.UserAgent,
			selectedQuery,
			rs,
			filter,
		)
		isHTML = true
	}

	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	if !a.handleUpstreamStatus(w, body, status) {
		return
	}

	// 5. Extract results based on filter
	var finalResults any
	var resultCount int

	switch filter {
	case services.FilterBoards:
		var boards []parsers.BoardMetadata
		if isHTML {
			boards, err = parsers.ExtractBoardsFromHTML(string(body))
		} else {
			boards, err = parsers.ExtractBoardsFromJSON(string(body))
		}
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}
		finalResults = boards
		resultCount = len(boards)

	case services.FilterUsers:
		var users []parsers.UserMetadata
		if isHTML {
			users, err = parsers.ExtractUsersFromHTML(string(body))
		} else {
			users, err = parsers.ExtractUsersFromJSON(string(body))
		}
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}
		finalResults = users
		resultCount = len(users)

	default:
		// Default to pins (including videos and products)
		var pins []map[string]any
		if isHTML {
			pins, err = parsers.ExtractSearchPinsFromHTML(string(body))
		} else {
			pins, err = parsers.ExtractSearchPinsFromJSON(string(body))
		}
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}

		// 6. Deduplicate against global store
		filteredPins := make([]map[string]any, 0, len(pins))
		for _, pin := range pins {
			id, ok := pin["id"].(string)
			if !ok || id == "" {
				continue
			}
			seen, _ := a.Store.IsPinSeen(r.Context(), id)
			if !seen {
				filteredPins = append(filteredPins, pin)
				_ = a.Store.MarkPinSeen(r.Context(), id)
			}
		}
		finalResults = filteredPins
		resultCount = len(filteredPins)
	}

	// 7. Update bookmark for the selected variant
	if nextBookmark != "" && nextBookmark != bookmark {
		_ = a.Store.UpdateSearchBookmark(r.Context(), selectedQuery, nextBookmark)
	}

	response := map[string]any{
		"count":          resultCount,
		"query_used":     selectedQuery,
		"original_query": query,
		"filter":         filter,
	}
	
	// Add results with appropriate key
	switch filter {
	case services.FilterBoards:
		response["boards"] = finalResults
	case services.FilterUsers:
		response["users"] = finalResults
	default:
		response["pins"] = finalResults
	}

	writeJSON(w, http.StatusOK, response)
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

func (a *App) React(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{
			"error": "method not allowed",
		})
		return
	}

	action, err := parseReactActionFromQuery(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	pinID := strings.TrimSpace(r.URL.Query().Get("id"))
	if pinID == "" {
		rawURL := strings.TrimSpace(r.URL.Query().Get("url"))
		if rawURL == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing id or url"})
			return
		}
		pinID = services.ExtractPinIDFromURL(rawURL)
		if pinID == "" {
			pinID = services.ExtractPinID(rawURL)
		}
	}
	if pinID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": services.ErrMissingPinID.Error()})
		return
	}

	// For check action, session is optional but recommended
	// For like/unlike actions, session is required
	var session *storage.Session
	
	if action != "check" {
		session, err = a.requireSession(w, r)
		if err != nil {
			return
		}
	} else {
		// For check, try to get session but don't require it
		if a.Store != nil {
			session, _ = a.Store.GetSession(r.Context(), storage.DefaultSessionID)
		}
	}

	var result *services.LikeResponse
	switch action {
	case "like":
		result, err = services.LikePin(r.Context(), a.httpClient(), session.CookiesHeader, session.HeadersJSON, session.UserAgent, pinID)
	case "unlike":
		result, err = services.UnlikePin(r.Context(), a.httpClient(), session.CookiesHeader, session.HeadersJSON, session.UserAgent, pinID)
	case "check":
		result, err = services.CheckLikeStatus(r.Context(), a.httpClient(), session.CookiesHeader, session.HeadersJSON, session.UserAgent, pinID)
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": services.ErrInvalidAction.Error()})
		return
	}
	if err != nil {
		switch {
		case errors.Is(err, services.ErrMissingPinID),
			errors.Is(err, services.ErrMissingAction),
			errors.Is(err, services.ErrInvalidAction):
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		case errors.Is(err, services.ErrCheckFailed):
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		default:
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}
	}

	writeJSON(w, http.StatusOK, result)
}

func parseReactActionFromQuery(r *http.Request) (string, error) {
	like := r.URL.Query().Has("like")
	unlike := r.URL.Query().Has("unlike")
	check := r.URL.Query().Has("check")
	if like && unlike {
		return "", errors.New("cannot specify both like and unlike")
	}
	if like && check {
		return "", errors.New("cannot specify both like and check")
	}
	if unlike && check {
		return "", errors.New("cannot specify both unlike and check")
	}
	if like {
		return "like", nil
	}
	if unlike {
		return "unlike", nil
	}
	if check {
		return "check", nil
	}
	return "", errors.New("must specify either like, unlike, or check")
}

func (a *App) Follow(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{
			"error": "method not allowed",
		})
		return
	}

	action, err := parseFollowActionFromQuery(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	userID := strings.TrimSpace(r.URL.Query().Get("id"))
	if userID == "" {
		profileURL := strings.TrimSpace(r.URL.Query().Get("url"))
		if profileURL == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing id or url"})
			return
		}

		// If only URL is provided, we need to fetch the ID first
		client := a.httpClient()
		session, _ := a.Store.GetSession(r.Context(), storage.DefaultSessionID)
		var cookiesHeader string
		if session != nil {
			cookiesHeader = session.CookiesHeader
		}

		username := parsers.ExtractUsername(profileURL)
		if username == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid profile url"})
			return
		}

		meta, err := services.FetchUserMetadata(r.Context(), client, cookiesHeader, username, "/")
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": "failed to resolve user id: " + err.Error()})
			return
		}
		userID = meta.ID
	}

	session, err := a.requireSession(w, r)
	if err != nil {
		return
	}

	var result *services.FollowResponse
	switch action {
	case "follow":
		result, err = services.FollowUser(r.Context(), a.httpClient(), session.CookiesHeader, session.HeadersJSON, session.UserAgent, userID)
	case "unfollow":
		result, err = services.UnfollowUser(r.Context(), a.httpClient(), session.CookiesHeader, session.HeadersJSON, session.UserAgent, userID)
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid action"})
		return
	}

	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, result)
}

func parseFollowActionFromQuery(r *http.Request) (string, error) {
	follow := r.URL.Query().Has("follow")
	unfollow := r.URL.Query().Has("unfollow")
	if follow && unfollow {
		return "", errors.New("cannot specify both follow and unfollow")
	}
	if follow {
		return "follow", nil
	}
	if unfollow {
		return "unfollow", nil
	}
	return "", errors.New("must specify either follow or unfollow")
}



func parseSearchRequest(w http.ResponseWriter, r *http.Request) (string, string, string, bool) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if query == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing q"})
		return "", "", "", false
	}
	rs := strings.TrimSpace(r.URL.Query().Get("rs"))
	if rs == "" {
		rs = "typed"
	}
	filter := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("filter")))
	if filter == "" {
		filter = services.FilterPins
	}
	return query, rs, filter, true
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
	if err == nil && !storage.SessionExpired(session) {
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
