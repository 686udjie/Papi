package services

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"

	"papi/parsers"
)

const defaultSearchRS = "typed"

func BuildSearchSourceURL(query string, rs string) string {
	if rs == "" {
		rs = defaultSearchRS
	}
	values := url.Values{}
	values.Set("q", query)
	values.Set("rs", rs)
	return "/search/pins/?" + values.Encode()
}

func FetchSearchPage(ctx context.Context, client *http.Client, cookiesHeader string, headersJSON string, userAgent string, query string, rs string) ([]byte, int, error) {
	if client == nil {
		return nil, 0, errors.New("http client is nil")
	}
	if query == "" {
		return nil, 0, errors.New("missing search query")
	}

	sourceURL := BuildSearchSourceURL(query, rs)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, HomefeedBaseURL+sourceURL, nil)
	if err != nil {
		return nil, 0, err
	}

	applyCapturedHeaders(req, headersJSON)
	if userAgent != "" {
		req.Header.Set("User-Agent", userAgent)
	} else {
		req.Header.Set("User-Agent", defaultUserAgent)
	}
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", defaultAcceptLang)
	req.Header.Set("Referer", defaultReferer)
	if cookiesHeader != "" {
		req.Header.Set("Cookie", cookiesHeader)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, err
	}

	return body, resp.StatusCode, nil
}

func ExtractSearchPinsJSON(html string) ([]byte, error) {
	pins, err := parsers.ExtractSearchPinsFromHTML(html)
	if err != nil {
		return nil, err
	}

	return json.Marshal(map[string]any{
		"pins":  pins,
		"count": len(pins),
	})
}
