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
	"time"
)

var nowFunc = time.Now

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
	applyDefaultHeaders(req, sourceURL, r.userAgent, r.cookiesHeader, "")

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
