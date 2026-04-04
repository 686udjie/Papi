package internal

import (
	"errors"
	"io"
	"net/http"
	"regexp"
	"time"
)

func FetchHTML(url string) (string, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")

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
