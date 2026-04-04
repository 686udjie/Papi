package services

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

var HomefeedBaseURL = "https://www.pinterest.com"
var nowFunc = time.Now

const (
	defaultAccept     = "application/json, text/javascript, */*, q=0.01"
	defaultReferer    = "https://www.pinterest.com/"
	defaultOrigin     = "https://www.pinterest.com"
	defaultUserAgent  = "Mozilla/5.0"
	defaultSourceURL  = "/"
	defaultAcceptLang = "en-US,en;q=0.9"
	defaultAppState   = "active"
	defaultPWSHandler = "www/index.js"
	defaultRetryDelay = 1 * time.Second
	defaultMaxRetries = 1
)

type homefeedPayload struct {
	Options struct {
		FieldSetKey string   `json:"field_set_key"`
		InNux       bool     `json:"in_nux"`
		InNewsHub   bool     `json:"in_news_hub"`
		StaticFeed  bool     `json:"static_feed"`
		ExcludeTabs bool     `json:"exclude_tabs"`
		Bookmarks   []string `json:"bookmarks,omitempty"`
	} `json:"options"`
	Context map[string]any `json:"context"`
}

type homefeedPayloadOverrides struct {
	Options struct {
		FieldSetKey *string `json:"field_set_key"`
		InNux       *bool   `json:"in_nux"`
		InNewsHub   *bool   `json:"in_news_hub"`
		StaticFeed  *bool   `json:"static_feed"`
		ExcludeTabs *bool   `json:"exclude_tabs"`
	} `json:"options"`
}

func BuildHomefeedData(bookmark string, dataJSON string) (string, error) {
	payload, err := buildHomefeedPayload(bookmark, dataJSON)
	if err != nil {
		return "", err
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	if len(raw) == 0 {
		return "", errors.New("failed to build payload")
	}
	encoded := url.QueryEscape(string(raw))
	return encoded, nil
}

func homefeedURL(encodedData string, sourceURL string, timestampMs int64) string {
	if timestampMs <= 0 {
		timestampMs = nowFunc().UnixMilli()
	}
	return HomefeedBaseURL + "/resource/UserHomefeedResource/get/?source_url=" + url.QueryEscape(sourceURL) + "&data=" + encodedData + "&_=" + strconv.FormatInt(timestampMs, 10)
}

func FetchHomefeed(ctx context.Context, client *http.Client, cookiesHeader string, bookmark string, headersJSON string, userAgent string, dataJSON string, sourceURL string) ([]byte, string, int, error) {
	if client == nil {
		return nil, "", 0, errors.New("http client is nil")
	}
	req := homefeedRequest{
		ctx:           ctx,
		client:        client,
		cookiesHeader: cookiesHeader,
		bookmark:      bookmark,
		headersJSON:   headersJSON,
		userAgent:     userAgent,
		dataJSON:      dataJSON,
		sourceURL:     sourceURL,
	}
	return req.fetchWithRetry(defaultMaxRetries)
}

type homefeedRequest struct {
	ctx           context.Context
	client        *http.Client
	cookiesHeader string
	bookmark      string
	headersJSON   string
	userAgent     string
	dataJSON      string
	sourceURL     string
}

func (r homefeedRequest) fetchWithRetry(retries int) ([]byte, string, int, error) {
	var lastStatus int
	for attempt := 0; attempt <= retries; attempt++ {
		body, nextBookmark, status, err := r.do()
		if err != nil {
			return nil, "", status, err
		}
		lastStatus = status
		if status != http.StatusForbidden || attempt == retries {
			return body, nextBookmark, status, nil
		}
		if err := sleepWithContext(r.ctx, defaultRetryDelay); err != nil {
			return nil, "", status, err
		}
	}
	return nil, "", lastStatus, errors.New("homefeed request failed after retries")
}

func (r homefeedRequest) do() ([]byte, string, int, error) {
	encoded, err := BuildHomefeedData(r.bookmark, r.dataJSON)
	if err != nil {
		return nil, "", 0, err
	}
	sourceURL := normalizeSourceURL(r.sourceURL)
	req, err := http.NewRequestWithContext(r.ctx, http.MethodGet, homefeedURL(encoded, sourceURL, nowFunc().UnixMilli()), nil)
	if err != nil {
		return nil, "", 0, err
	}
	applyCapturedHeaders(req, r.headersJSON)
	applyDefaultHeaders(req, sourceURL, r.userAgent, r.cookiesHeader)

	resp, err := r.client.Do(req)
	if err != nil {
		return nil, "", 0, err
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", resp.StatusCode, err
	}

	nextBookmark, _ := ParseHomefeedBookmark(body)
	return body, nextBookmark, resp.StatusCode, nil
}

func normalizeSourceURL(sourceURL string) string {
	if sourceURL == "" {
		return defaultSourceURL
	}
	return sourceURL
}

func applyDefaultHeaders(req *http.Request, sourceURL string, userAgent string, cookiesHeader string) {
	if req == nil {
		return
	}
	setHeaderIfEmpty(req.Header, "Accept", defaultAccept)
	setHeaderIfEmpty(req.Header, "Referer", defaultReferer)
	setHeaderIfEmpty(req.Header, "Origin", defaultOrigin)
	setHeaderIfEmpty(req.Header, "X-Requested-With", "XMLHttpRequest")
	setHeaderIfEmpty(req.Header, "X-Pinterest-Source-URL", sourceURL)
	setHeaderIfEmpty(req.Header, "X-Pinterest-Source-Url", sourceURL)
	setHeaderIfEmpty(req.Header, "X-Pinterest-AppState", defaultAppState)
	setHeaderIfEmpty(req.Header, "X-Pinterest-PWS-Handler", defaultPWSHandler)
	setHeaderIfEmpty(req.Header, "Accept-Language", defaultAcceptLang)
	if userAgent != "" {
		req.Header.Set("User-Agent", userAgent)
	} else {
		setHeaderIfEmpty(req.Header, "User-Agent", defaultUserAgent)
	}
	if cookiesHeader != "" {
		req.Header.Set("Cookie", cookiesHeader)
	}
	if token := extractCSRFFromCookies(cookiesHeader); token != "" {
		req.Header.Set("X-CSRFToken", token)
	}
}

func setHeaderIfEmpty(header http.Header, key string, value string) {
	if header.Get(key) == "" && value != "" {
		header.Set(key, value)
	}
}

func sleepWithContext(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func buildHomefeedPayload(bookmark string, dataJSON string) (homefeedPayload, error) {
	payload := homefeedPayload{
		Context: map[string]any{},
	}
	payload.Options.FieldSetKey = "hf_grid"
	payload.Options.InNux = false
	payload.Options.InNewsHub = false
	payload.Options.StaticFeed = false
	payload.Options.ExcludeTabs = true

	if dataJSON != "" {
		var overrides homefeedPayloadOverrides
		if err := json.Unmarshal([]byte(dataJSON), &overrides); err != nil {
			return homefeedPayload{}, err
		}
		if overrides.Options.FieldSetKey != nil && *overrides.Options.FieldSetKey != "" {
			payload.Options.FieldSetKey = *overrides.Options.FieldSetKey
		}
		if overrides.Options.InNux != nil {
			payload.Options.InNux = *overrides.Options.InNux
		}
		if overrides.Options.InNewsHub != nil {
			payload.Options.InNewsHub = *overrides.Options.InNewsHub
		}
		if overrides.Options.StaticFeed != nil {
			payload.Options.StaticFeed = *overrides.Options.StaticFeed
		}
		if overrides.Options.ExcludeTabs != nil {
			payload.Options.ExcludeTabs = *overrides.Options.ExcludeTabs
		}
	}

	if bookmark != "" {
		payload.Options.Bookmarks = []string{bookmark}
	}

	return payload, nil
}

func ParseHomefeedBookmark(body []byte) (string, error) {
	body = bytes.TrimSpace(body)
	if len(body) == 0 {
		return "", errors.New("empty response body")
	}

	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		return "", err
	}

	resource, ok := raw["resource_response"].(map[string]any)
	if !ok {
		return "", errors.New("resource_response missing")
	}
	if bookmark, ok := resource["bookmark"].(string); ok && bookmark != "" {
		return bookmark, nil
	}
	if bookmarks, ok := resource["bookmarks"].([]any); ok && len(bookmarks) > 0 {
		if s, ok := bookmarks[0].(string); ok && s != "" {
			return s, nil
		}
	}
	return "", errors.New("bookmark missing")
}

func extractCSRFFromCookies(cookiesHeader string) string {
	if cookiesHeader == "" {
		return ""
	}
	parts := strings.Split(cookiesHeader, ";")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, "csrftoken=") {
			return strings.TrimPrefix(part, "csrftoken=")
		}
	}
	return ""
}

func applyCapturedHeaders(req *http.Request, headersJSON string) {
	if headersJSON == "" || req == nil {
		return
	}
	var headers map[string]string
	if err := json.Unmarshal([]byte(headersJSON), &headers); err != nil {
		return
	}
	for key, value := range headers {
		if key == "" || value == "" {
			continue
		}
		switch strings.ToLower(key) {
		case "cookie", "host", "content-length", "connection":
			continue
		}
		req.Header.Set(key, value)
	}
}
