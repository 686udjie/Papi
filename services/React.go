package services

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

const (
	PinterestAPIBase    = "https://www.pinterest.com/resource"
	APICreateEndpoint   = "/ApiResource/create/"
	APIDeleteEndpoint   = "/ApiResource/delete/"
	ClientTrackingParam = "CwABAAAAEDE2NjIxMzA5MjY0MjU3MzAGAAMABwsABwAAAApuZ2FwaS9wcm9kCwAOAAAAC2RvY2tlci1wcm9kCwAPAAAACXVuaXR5LXAycAwAEAgAAQAAAAMCAAMBAAA"
	LikeReactionType    = 1
	PinURLTemplate      = "/pin/%s/"
	PinResourceTemplate = "/v3/pins/%s/react/"
	ScriptName          = "www/pin/[id].js"
	ContentType         = "application/x-www-form-urlencoded; charset=UTF-8"
)

var (
	ErrMissingPinID  = errors.New("missing pin id")
	ErrInvalidAction = errors.New("invalid action, must be like or unlike")
	ErrMissingAction = errors.New("missing action parameter")
	ErrLikeFailed    = errors.New("failed to like pin")
	ErrUnlikeFailed  = errors.New("failed to unlike pin")
)

type LikeResponse struct {
	State string `json:"state,omitempty"`
	PinID string `json:"pin_id,omitempty"`
}

func LikePin(ctx context.Context, client *http.Client, cookiesHeader, headersJSON, userAgent, pinID string) (*LikeResponse, error) {
	return performCreateReact(ctx, client, cookiesHeader, headersJSON, userAgent, pinID, LikeReactionType, "like")
}

func UnlikePin(ctx context.Context, client *http.Client, cookiesHeader, headersJSON, userAgent, pinID string) (*LikeResponse, error) {
	return performDeleteReact(ctx, client, cookiesHeader, headersJSON, userAgent, pinID)
}

func unmarshalPossiblyWrappedJSON(body []byte, target any) error {
	if len(body) == 0 {
		return errors.New("empty body")
	}
	trimmed := bytes.TrimSpace(body)
	if err := json.Unmarshal(trimmed, target); err == nil {
		return nil
	}

	// Pinterest sometimes prefixes responses (e.g. "for (;;);") or returns extra bytes.
	start := bytes.IndexByte(trimmed, '{')
	end := bytes.LastIndexByte(trimmed, '}')
	if start >= 0 && end > start {
		candidate := trimmed[start : end+1]
		if err := json.Unmarshal(candidate, target); err == nil {
			return nil
		}
	}
	return errors.New("invalid json")
}

func buildReactPayload(pinID string, includeReactionType bool) (map[string]any, error) {
	data := map[string]any{
		"client_tracking_params": ClientTrackingParam,
	}
	
	if includeReactionType {
		data["reaction_type"] = LikeReactionType
	}
	
	options := map[string]any{
		"url":  fmt.Sprintf(PinResourceTemplate, pinID),
		"data": data,
	}
	
	return map[string]any{
		"options": options,
		"context": map[string]any{},
	}, nil
}

func createReactRequest(ctx context.Context, endpoint, pinID string, payload map[string]any) (*http.Request, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal payload: %w", err)
	}

	form := url.Values{}
	form.Set("source_url", fmt.Sprintf(PinURLTemplate, pinID))
	form.Set("data", string(raw))

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, PinterestAPIBase+endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", ContentType)
	return req, nil
}

func executeRequest(client *http.Client, req *http.Request) ([]byte, error) {
	if client == nil {
		client = http.DefaultClient
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("upstream returned status %s, body: %s", resp.Status, string(body))
	}

	return io.ReadAll(resp.Body)
}

func validateResponse(body []byte) error {
	var response map[string]any
	if err := json.Unmarshal(body, &response); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	if resourceData, ok := response["resource_response"].(map[string]any); ok {
		if status, ok := resourceData["status"].(string); ok {
			if status == "success" {
				return nil
			}
		}
	}
	return errors.New("operation not successful")
}

func performCreateReact(ctx context.Context, client *http.Client, cookiesHeader, headersJSON, userAgent, pinID string, reactionType int, action string) (*LikeResponse, error) {
	if pinID == "" {
		return nil, ErrMissingPinID
	}

	payload, err := buildReactPayload(pinID, true)
	if err != nil {
		return nil, err
	}

	req, err := createReactRequest(ctx, APICreateEndpoint, pinID, payload)
	if err != nil {
		return nil, err
	}

	applyDefaultHeaders(req, fmt.Sprintf(PinURLTemplate, pinID), userAgent, cookiesHeader, ScriptName)
	applyCapturedHeaders(req, headersJSON)

	body, err := executeRequest(client, req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", reactionErrorForAction(action), err)
	}

	if err := validateResponse(body); err != nil {
		return nil, fmt.Errorf("%w: %v", reactionErrorForAction(action), err)
	}

	state := "liked"
	if reactionType != 1 {
		state = "unliked"
	}

	return &LikeResponse{
		State: state,
		PinID: pinID,
	}, nil
}

func performDeleteReact(ctx context.Context, client *http.Client, cookiesHeader, headersJSON, userAgent, pinID string) (*LikeResponse, error) {
	if pinID == "" {
		return nil, ErrMissingPinID
	}

	payload, err := buildReactPayload(pinID, false)
	if err != nil {
		return nil, err
	}

	req, err := createReactRequest(ctx, APIDeleteEndpoint, pinID, payload)
	if err != nil {
		return nil, err
	}

	applyDefaultHeaders(req, fmt.Sprintf(PinURLTemplate, pinID), userAgent, cookiesHeader, ScriptName)
	applyCapturedHeaders(req, headersJSON)

	body, err := executeRequest(client, req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnlikeFailed, err)
	}

	if err := validateResponse(body); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnlikeFailed, err)
	}

	return &LikeResponse{
		State: "unliked",
		PinID: pinID,
	}, nil
}

func reactionErrorForAction(action string) error {
	switch action {
	case "unlike":
		return ErrUnlikeFailed
	default:
		return ErrLikeFailed
	}
}

func ExtractPinIDFromURL(rawURL string) string {
	if rawURL == "" {
		return ""
	}

	// Extract pin ID from URL patterns like /pin/123456/
	parts := strings.Split(rawURL, "/")
	for i, part := range parts {
		if part == "pin" && i+1 < len(parts) {
			pinID := parts[i+1]
			// Validate that it's a numeric ID
			if _, err := strconv.Atoi(pinID); err == nil {
				return pinID
			}
		}
	}
	return ""
}
