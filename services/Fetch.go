package services

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

var HomefeedBaseURL = "https://www.pinterest.com"

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

func FetchHTML(url string) (string, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", defaultUserAgent)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", errors.New("upstream returned status " + resp.Status)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(body), nil
}

func ExtractPinID(raw string) string {
	re := regexp.MustCompile(`/pin/(\d+)/`)
	match := re.FindStringSubmatch(raw)
	if len(match) > 1 {
		return match[1]
	}
	return ""
}

func applyDefaultHeaders(req *http.Request, sourceURL string, userAgent string, cookiesHeader string, pwsHandler string) {
	if req == nil {
		return
	}
	if pwsHandler == "" {
		pwsHandler = defaultPWSHandler
	}

	setHeaderIfEmpty(req.Header, "Accept", defaultAccept)
	setHeaderIfEmpty(req.Header, "Referer", defaultReferer)
	setHeaderIfEmpty(req.Header, "Origin", defaultOrigin)
	setHeaderIfEmpty(req.Header, "X-Requested-With", "XMLHttpRequest")
	setHeaderIfEmpty(req.Header, "X-Pinterest-Source-URL", sourceURL)
	setHeaderIfEmpty(req.Header, "X-Pinterest-Source-Url", sourceURL)
	setHeaderIfEmpty(req.Header, "X-Pinterest-AppState", defaultAppState)
	setHeaderIfEmpty(req.Header, "X-Pinterest-PWS-Handler", pwsHandler)
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

func fetchResource(ctx context.Context, client *http.Client, cookiesHeader, endpoint string, sourceURL string, pwsHandler string, options map[string]any) ([]byte, error) {
	payload := map[string]any{
		"options": options,
		"context": map[string]any{},
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	u, _ := url.Parse("https://www.pinterest.com/resource/" + endpoint + "/")
	q := u.Query()
	q.Set("source_url", sourceURL)
	q.Set("data", string(raw))
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		return nil, err
	}
	applyDefaultHeaders(req, sourceURL, "", cookiesHeader, pwsHandler)

	if client == nil {
		client = http.DefaultClient
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, errors.New("upstream returned status " + resp.Status)
	}

	return io.ReadAll(resp.Body)
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
