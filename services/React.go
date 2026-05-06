package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
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
	likeState = make(map[string]bool)
	stateMutex sync.RWMutex
)

var (
	ErrMissingPinID  = errors.New("missing pin id")
	ErrInvalidAction = errors.New("invalid action, must be like, unlike, or check")
	ErrMissingAction = errors.New("missing action parameter")
	ErrLikeFailed    = errors.New("failed to like pin")
	ErrUnlikeFailed  = errors.New("failed to unlike pin")
	ErrCheckFailed   = errors.New("failed to check like status")
)

type LikeResponse struct {
	State  string `json:"state,omitempty"`
	PinID  string `json:"pin_id,omitempty"`
	Action string `json:"action,omitempty"`
}

func LikePin(ctx context.Context, client *http.Client, cookiesHeader, headersJSON, userAgent, pinID string) (*LikeResponse, error) {
	if pinID == "" {
		return nil, ErrMissingPinID
	}

	err := performReact(ctx, client, cookiesHeader, headersJSON, userAgent, pinID, true)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrLikeFailed, err)
	}

	stateMutex.Lock()
	likeState[pinID] = true
	stateMutex.Unlock()

	return &LikeResponse{
		State: "liked",
		PinID: pinID,
	}, nil
}

func UnlikePin(ctx context.Context, client *http.Client, cookiesHeader, headersJSON, userAgent, pinID string) (*LikeResponse, error) {
	if pinID == "" {
		return nil, ErrMissingPinID
	}

	err := performReact(ctx, client, cookiesHeader, headersJSON, userAgent, pinID, false)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnlikeFailed, err)
	}

	// Update local state
	stateMutex.Lock()
	likeState[pinID] = false
	stateMutex.Unlock()

	return &LikeResponse{
		State: "unliked",
		PinID: pinID,
	}, nil
}

func CheckLikeStatus(ctx context.Context, client *http.Client, cookiesHeader, headersJSON, userAgent, pinID string) (*LikeResponse, error) {
	if pinID == "" {
		return nil, ErrMissingPinID
	}

	stateMutex.RLock()
	liked := likeState[pinID]
	stateMutex.RUnlock()

	state := "unliked"
	if liked {
		state = "liked"
	}

	return &LikeResponse{
		State: state,
		PinID: pinID,
	}, nil
}

func performReact(ctx context.Context, client *http.Client, cookiesHeader, headersJSON, userAgent, pinID string, isLike bool) error {
	if pinID == "" {
		return ErrMissingPinID
	}

	data := map[string]any{
		"client_tracking_params": ClientTrackingParam,
	}
	if isLike {
		data["reaction_type"] = LikeReactionType
	}

	options := map[string]any{
		"url":  fmt.Sprintf(PinResourceTemplate, pinID),
		"data": data,
	}

	payload := map[string]any{
		"options": options,
		"context": map[string]any{},
	}

	raw, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	form := url.Values{}
	form.Set("source_url", fmt.Sprintf(PinURLTemplate, pinID))
	form.Set("data", string(raw))

	endpoint := APICreateEndpoint
	if !isLike {
		endpoint = APIDeleteEndpoint
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, PinterestAPIBase+endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	applyDefaultHeaders(req, fmt.Sprintf(PinURLTemplate, pinID), userAgent, cookiesHeader, ScriptName)
	applyCapturedHeaders(req, headersJSON)
	req.Header.Set("Content-Type", ContentType)

	if client == nil {
		client = http.DefaultClient
	}

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("upstream returned status %s", resp.Status)
	}

	return nil
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
