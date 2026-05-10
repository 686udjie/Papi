package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const (
	PinterestAppVersion = "836eddf"
	DefaultCTP          = "CwABAAAAEDE3MjM1MzEwMTU4Nzc1NjUKAAIAAAGeE1t01AYAAwAACgAGAAAAAAAAANwLAAcAAAAKbmdhcGkvcHJvZAsADgAAAAtkb2NrZXItcHJvZAsADwAAAA51bml0eS1ob21lZmVlZAwAEAgAAQAAAAUCAAMBAAA"
	RepinEndpoint       = "RepinResource/create"
	UnsaveEndpoint      = "PinResource/delete"
	TrackingEndpoint    = "ActiveUserResource/create"
)

var (
	ErrSaveFailed   = errors.New("failed to save pin")
	ErrUnsaveFailed = errors.New("failed to unsave pin")
)

type SaveResponse struct {
	Status string `json:"status"`
	PinID  string `json:"pin_id"`
	SaveID string `json:"save_id,omitempty"`
}

type pinMetadata struct {
	Title   string
	CTP     string
	RepinID string
	IsRepin bool
}

// SavePin saves a pin to the user's profile (Quick Save).
func SavePin(ctx context.Context, client *http.Client, cookiesHeader, headersJSON, userAgent, pinID string) (*SaveResponse, error) {
	if pinID == "" {
		return nil, ErrMissingPinID
	}

	// 1. Resolve metadata (title and CTP) from the pin page
	meta, _ := fetchPinMetadata(ctx, client, cookiesHeader, headersJSON, userAgent, pinID)
	
	title := ""
	ctp := DefaultCTP
	if meta != nil {
		if meta.Title != "" {
			title = meta.Title
		}
		if meta.CTP != "" {
			ctp = meta.CTP
		}
	}

	sourceURL := fmt.Sprintf("/pin/%s/", pinID)

	// 2. Send tracking event (Engagement)
	sendTrackingEvent(ctx, client, cookiesHeader, headersJSON, userAgent, sourceURL)

	// 3. Perform the actual Save
	options := map[string]any{
		"pin_id":               pinID,
		"carousel_slot_index":  0,
		"clientTrackingParams": ctp,
		"description":          " ",
		"is_buyable_pin":       false,
		"is_promoted":          false,
		"is_removable":         false,
		"link":                 nil,
		"title":                title,
		"aux_data": map[string]any{
			"source": "deep_linking",
		},
	}

	body, err := callPinterestResource(ctx, client, RepinEndpoint, sourceURL, cookiesHeader, headersJSON, userAgent, options)
	if err != nil {
		return nil, err
	}

	var rawResp map[string]any
	if err := json.Unmarshal(body, &rawResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	saveID := ""
	if resourceResp, ok := rawResp["resource_response"].(map[string]any); ok {
		if data, ok := resourceResp["data"].(map[string]any); ok {
			if id, ok := data["id"].(string); ok {
				saveID = id
			}
		}
	}

	return &SaveResponse{
		Status: "saved",
		PinID:  pinID,
		SaveID: saveID,
	}, nil
}

// UnsavePin removes a saved pin from the user's profile.
func UnsavePin(ctx context.Context, client *http.Client, cookiesHeader, headersJSON, userAgent, id string) (*SaveResponse, error) {
	if id == "" {
		return nil, errors.New("missing id")
	}

	saveID := id
	ctp := DefaultCTP
	
	// 1. Resolve metadata (CTP and repin_id if necessary)
	meta, _ := fetchPinMetadata(ctx, client, cookiesHeader, headersJSON, userAgent, id)
	if meta != nil {
		if meta.RepinID != "" {
			saveID = meta.RepinID
		}
		if meta.CTP != "" {
			ctp = meta.CTP
		}
	}

	options := map[string]any{
		"id":                     saveID,
		"client_tracking_params": ctp,
	}

	sourceURL := fmt.Sprintf("/pin/%s/", saveID)

	// 2. Send tracking event (ActiveUserResource/create)
	sendTrackingEvent(ctx, client, cookiesHeader, headersJSON, userAgent, sourceURL)

	// 3. Perform the actual Unsave
	_, err := callPinterestResource(ctx, client, UnsaveEndpoint, sourceURL, cookiesHeader, headersJSON, userAgent, options)
	if err != nil {
		return nil, err
	}

	return &SaveResponse{
		Status: "unsaved",
		PinID:  id,
		SaveID: saveID,
	}, nil
}

// CheckPinStatus returns the save_id/repin_id if the pin is currently saved by the user.
func CheckPinStatus(ctx context.Context, client *http.Client, cookiesHeader, headersJSON, userAgent, pinID string) (*SaveResponse, error) {
	if pinID == "" {
		return nil, ErrMissingPinID
	}

	meta, err := fetchPinMetadata(ctx, client, cookiesHeader, headersJSON, userAgent, pinID)
	if err != nil {
		return nil, err
	}

	status := "not_saved"
	saveID := meta.RepinID
	if meta.RepinID != "" {
		status = "saved"
	} else if meta.IsRepin {
		status = "saved"
		saveID = pinID
	}

	return &SaveResponse{
		Status: status,
		PinID:  pinID,
		SaveID: saveID,
	}, nil
}

// Internal helpers

func callPinterestResource(ctx context.Context, client *http.Client, endpoint, sourceURL, cookiesHeader, headersJSON, userAgent string, options map[string]any) ([]byte, error) {
	if client == nil {
		client = http.DefaultClient
	}

	payload := map[string]any{
		"options": options,
		"context": map[string]any{},
	}

	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	form := url.Values{}
	form.Set("source_url", sourceURL)
	form.Set("data", string(raw))

	apiURL := fmt.Sprintf("https://www.pinterest.com/resource/%s/", endpoint)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}

	applyDefaultHeaders(req, sourceURL, userAgent, cookiesHeader, "www/pin/[id].js")
	applyCapturedHeaders(req, headersJSON)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8")
	if req.Header.Get("X-App-Version") == "" {
		req.Header.Set("X-App-Version", PinterestAppVersion)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("upstream returned status %s: %s", resp.Status, string(body))
	}

	return body, nil
}

func sendTrackingEvent(ctx context.Context, client *http.Client, cookiesHeader, headersJSON, userAgent, sourceURL string) {
	options := map[string]any{
		"data": map[string]any{
			"appVersion": PinterestAppVersion,
			"auxData": map[string]any{
				"stage": "prod",
			},
			"browser":        1,
			"clientUUID":     "c837c004-614d-4e70-8bd9-1f3c3edf2189",
			"event_type":     7137,
			"time":           time.Now().UnixNano() / 1000000,
			"unauth_id":      "27dfa7bafd8a4b718fe0f3d1d11c1308",
			"view_type":      3,
			"view_parameter": 156,
		},
	}
	// We ignore errors for tracking events
	_, _ = callPinterestResource(ctx, client, TrackingEndpoint, sourceURL, cookiesHeader, headersJSON, userAgent, options)
}

func fetchPinMetadata(ctx context.Context, client *http.Client, cookiesHeader, headersJSON, userAgent, pinID string) (*pinMetadata, error) {
	if client == nil {
		client = http.DefaultClient
	}

	pinURL := fmt.Sprintf("https://www.pinterest.com/pin/%s/", pinID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pinURL, nil)
	if err != nil {
		return nil, err
	}
	
	applyDefaultHeaders(req, "/", userAgent, cookiesHeader, "www/pin/[id].js")
	applyCapturedHeaders(req, headersJSON)
	req.Header.Set("X-App-Version", PinterestAppVersion)
	
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	
	body, _ := io.ReadAll(resp.Body)
	html := string(body)
	
	meta := &pinMetadata{}
	
	// Extract Title
	if strings.Contains(html, "\"title\":\"") {
		parts := strings.Split(html, "\"title\":\"")
		meta.Title = strings.Split(parts[1], "\"")[0]
	}
	
	// Extract CTP (clientTrackingParams / client_tracking_params)
	ctpPatterns := []string{"\"clientTrackingParams\":\"", "\"client_tracking_params\":\""}
	for _, p := range ctpPatterns {
		if strings.Contains(html, p) {
			parts := strings.Split(html, p)
			meta.CTP = strings.Split(parts[1], "\"")[0]
			break
		}
	}
	
	// 3. Extract RepinID (repin_id / native_pin_id)
	// We look for explicit repin fields. This is the most reliable way to find the save_id.
	re := regexp.MustCompile(`"(?:repin_id|native_pin_id)":\s*"?(\d{15,})"?`)
	matches := re.FindAllStringSubmatch(html, -1)
	
	for _, m := range matches {
		foundID := m[1]
		if foundID != pinID {
			meta.RepinID = foundID
			return meta, nil
		}
	}

	// 4. Detect if this pin is itself a repin
	if strings.Contains(html, "\"is_repin\":true") || strings.Contains(html, "\"is_repin\": true") {
		meta.IsRepin = true
	}

	return meta, nil
}
