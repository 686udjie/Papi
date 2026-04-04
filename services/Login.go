package services

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"time"

	"papi/storage"
)

const (
	loginBaseURL       = "https://www.pinterest.com"
	loginPath          = "/login/"
	loginEndpoint      = "https://www.pinterest.com/resource/UserSessionResource/create/"
	defaultLoginUA     = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/122.0.0.0 Safari/537.36"
	defaultLoginSource = "/login/"
)

var (
	ErrInvalidInput       = errors.New("invalid login input")
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrChallenge          = errors.New("pinterest blocked or challenged login")
	ErrUpstream           = errors.New("upstream error during login")
	ErrNetwork            = errors.New("network error during login")
)

type LoginResult struct {
	CookiesHeader string
	CookiesJSON   string
	UserAgent     string
	Bookmark      string
	SourceURL     string
}

type loginResponse struct {
	ResourceResponse struct {
		Data  any `json:"data"`
		Error any `json:"error"`
	} `json:"resource_response"`
}

func LoginAndCaptureSession(ctx context.Context, email string, password string, store storage.SessionStore) (*LoginResult, error) {
	email, password, err := validateCredentials(email, password)
	if err != nil {
		return nil, err
	}
	if store == nil {
		return nil, errors.New("session store is nil")
	}

	log.Println("login attempt")

	client, err := newLoginClient(defaultLoginUA)
	if err != nil {
		log.Printf("login failed: %v", err)
		return nil, ErrNetwork
	}

	if err := client.seedCookies(ctx); err != nil {
		log.Printf("login failed: %v", err)
		return nil, ErrNetwork
	}

	csrf := client.csrfToken()
	if csrf == "" {
		log.Printf("login failed: missing csrftoken")
		return nil, ErrUpstream
	}

	var respBody []byte
	var status int
	for attempt := 0; attempt < 2; attempt++ {
		respBody, status, err = client.sendLoginRequest(ctx, csrf, email, password)
		if err == nil {
			break
		}
		if !isRetryableLoginError(err) || attempt == 1 {
			log.Printf("login failed: %v", err)
			return nil, err
		}
	}

	if err := validateLoginResponse(respBody, status, client.jar); err != nil {
		log.Printf("login failed: %v", err)
		return nil, err
	}

	cookiesHeader := cookieHeaderFromJar(client.jar)
	cookiesJSON := cookiesJSONFromJar(client.jar)

	bookmark := ""
	sourceURL := defaultSourceURL
	_, nextBookmark, fetchStatus, fetchErr := FetchHomefeed(ctx, client.http, cookiesHeader, "", "", client.userAgent, "", sourceURL)
	if fetchErr != nil || fetchStatus < 200 || fetchStatus >= 300 {
		if fetchErr != nil {
			log.Printf("login succeeded but homefeed fetch failed: %v", fetchErr)
		} else {
			log.Printf("login succeeded but homefeed fetch failed: status %d", fetchStatus)
		}
	} else if nextBookmark != "" {
		bookmark = nextBookmark
	}

	session := &storage.Session{
		ID:            storage.DefaultSessionID,
		CookiesJSON:   cookiesJSON,
		CookiesHeader: cookiesHeader,
		HeadersJSON:   "",
		UserAgent:     defaultLoginUA,
		DataJSON:      "",
		SourceURL:     sourceURL,
		Bookmark:      bookmark,
		UpdatedAt:     time.Now().UTC(),
	}

	if err := store.UpsertSession(ctx, session); err != nil {
		log.Printf("login failed: %v", err)
		return nil, ErrUpstream
	}

	log.Println("login success")

	return &LoginResult{
		CookiesHeader: cookiesHeader,
		CookiesJSON:   cookiesJSON,
		UserAgent:     defaultLoginUA,
		Bookmark:      bookmark,
		SourceURL:     sourceURL,
	}, nil
}

type loginClient struct {
	http      *http.Client
	jar       *cookiejar.Jar
	userAgent string
}

func newLoginClient(userAgent string) (*loginClient, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}
	client := &http.Client{
		Jar:     jar,
		Timeout: 15 * time.Second,
	}
	return &loginClient{
		http:      client,
		jar:       jar,
		userAgent: userAgent,
	}, nil
}

func (c *loginClient) seedCookies(ctx context.Context) error {
	if err := c.seedRequest(ctx, loginBaseURL+"/"); err != nil {
		return err
	}
	if err := c.seedRequest(ctx, loginBaseURL+loginPath); err != nil {
		return err
	}
	return nil
}

func (c *loginClient) seedRequest(ctx context.Context, target string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Accept", "text/html")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	_, _ = io.Copy(io.Discard, resp.Body)
	return nil
}

func (c *loginClient) csrfToken() string {
	if c == nil || c.jar == nil {
		return ""
	}
	u, err := url.Parse(loginBaseURL)
	if err != nil {
		return ""
	}
	for _, cookie := range c.jar.Cookies(u) {
		if cookie.Name == "csrftoken" {
			return cookie.Value
		}
	}
	return ""
}

func (c *loginClient) sendLoginRequest(ctx context.Context, csrf string, email string, password string) ([]byte, int, error) {
	payload := map[string]any{
		"options": map[string]any{
			"username_or_email": email,
			"password":          password,
		},
		"context": map[string]any{},
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, 0, ErrNetwork
	}
	form := url.Values{}
	form.Set("source_url", defaultLoginSource)
	form.Set("data", string(raw))

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, loginEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, 0, ErrNetwork
	}
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8")
	req.Header.Set("X-CSRFToken", csrf)
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	req.Header.Set("Referer", loginBaseURL+loginPath)
	req.Header.Set("Origin", loginBaseURL)

	resp, err := c.http.Do(req)
	if err != nil {
		if isNetworkError(err) {
			return nil, 0, ErrNetwork
		}
		return nil, 0, ErrUpstream
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, ErrNetwork
	}
	if resp.StatusCode == http.StatusUnauthorized {
		return body, resp.StatusCode, ErrInvalidCredentials
	}
	if resp.StatusCode == http.StatusForbidden {
		return body, resp.StatusCode, ErrChallenge
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		return body, resp.StatusCode, ErrUpstream
	}
	if resp.StatusCode >= 500 {
		return body, resp.StatusCode, ErrUpstream
	}
	return body, resp.StatusCode, nil
}

func validateLoginResponse(body []byte, status int, jar *cookiejar.Jar) error {
	if status < 200 || status >= 300 {
		return ErrUpstream
	}
	if len(body) == 0 {
		return ErrUpstream
	}

	var parsed loginResponse
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if err := decoder.Decode(&parsed); err != nil {
		return ErrUpstream
	}
	if parsed.ResourceResponse.Data == nil {
		if parsed.ResourceResponse.Error != nil {
			return ErrInvalidCredentials
		}
		return ErrInvalidCredentials
	}
	if !hasSessionCookie(jar) {
		return ErrInvalidCredentials
	}
	return nil
}

func hasSessionCookie(jar *cookiejar.Jar) bool {
	u, err := url.Parse(loginBaseURL)
	if err != nil {
		return false
	}
	for _, c := range jar.Cookies(u) {
		if c.Name == "_pinterest_sess" {
			return true
		}
	}
	return false
}

func cookieHeaderFromJar(jar *cookiejar.Jar) string {
	u, err := url.Parse(loginBaseURL)
	if err != nil || jar == nil {
		return ""
	}
	cookies := jar.Cookies(u)
	if len(cookies) == 0 {
		return ""
	}
	parts := make([]string, 0, len(cookies))
	for _, c := range cookies {
		if c == nil || c.Name == "" {
			continue
		}
		parts = append(parts, c.Name+"="+c.Value)
	}
	return strings.Join(parts, "; ")
}

func cookiesJSONFromJar(jar *cookiejar.Jar) string {
	u, err := url.Parse(loginBaseURL)
	if err != nil || jar == nil {
		return "[]"
	}
	cookies := jar.Cookies(u)
	type cookieRecord struct {
		Name     string    `json:"name"`
		Value    string    `json:"value"`
		Path     string    `json:"path,omitempty"`
		Domain   string    `json:"domain,omitempty"`
		Expires  time.Time `json:"expires,omitempty"`
		Secure   bool      `json:"secure,omitempty"`
		HttpOnly bool      `json:"httpOnly,omitempty"`
		SameSite string    `json:"sameSite,omitempty"`
	}
	records := make([]cookieRecord, 0, len(cookies))
	for _, c := range cookies {
		if c == nil {
			continue
		}
		records = append(records, cookieRecord{
			Name:     c.Name,
			Value:    c.Value,
			Path:     c.Path,
			Domain:   c.Domain,
			Expires:  c.Expires,
			Secure:   c.Secure,
			HttpOnly: c.HttpOnly,
			SameSite: sameSiteString(c.SameSite),
		})
	}
	raw, err := json.Marshal(records)
	if err != nil {
		return "[]"
	}
	return string(raw)
}

func isRetryableLoginError(err error) bool {
	return errors.Is(err, ErrNetwork) || errors.Is(err, ErrUpstream)
}

func isNetworkError(err error) bool {
	var netErr net.Error
	return errors.As(err, &netErr)
}

func validateCredentials(email string, password string) (string, string, error) {
	email = strings.TrimSpace(email)
	if email == "" || !strings.Contains(email, "@") {
		return "", "", ErrInvalidInput
	}
	password = strings.TrimSpace(password)
	if password == "" {
		return "", "", ErrInvalidInput
	}
	return email, password, nil
}

func sameSiteString(value http.SameSite) string {
	switch value {
	case http.SameSiteStrictMode:
		return "Strict"
	case http.SameSiteLaxMode:
		return "Lax"
	case http.SameSiteNoneMode:
		return "None"
	default:
		return ""
	}
}
