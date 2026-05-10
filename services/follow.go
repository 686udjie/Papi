package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

var (
	ErrMissingUserID  = errors.New("missing user id")
	ErrFollowFailed   = errors.New("failed to follow user")
	ErrUnfollowFailed = errors.New("failed to unfollow user")
)

type FollowResponse struct {
	Status string `json:"status"`
	UserID string `json:"user_id"`
}

func FollowUser(ctx context.Context, client *http.Client, cookiesHeader, headersJSON, userAgent, userID string) (*FollowResponse, error) {
	if userID == "" {
		return nil, ErrMissingUserID
	}

	err := performFollowAction(ctx, client, cookiesHeader, headersJSON, userAgent, userID, true)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrFollowFailed, err)
	}

	return &FollowResponse{
		Status: "followed",
		UserID: userID,
	}, nil
}

func UnfollowUser(ctx context.Context, client *http.Client, cookiesHeader, headersJSON, userAgent, userID string) (*FollowResponse, error) {
	if userID == "" {
		return nil, ErrMissingUserID
	}

	err := performFollowAction(ctx, client, cookiesHeader, headersJSON, userAgent, userID, false)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnfollowFailed, err)
	}

	return &FollowResponse{
		Status: "unfollowed",
		UserID: userID,
	}, nil
}

func performFollowAction(ctx context.Context, client *http.Client, cookiesHeader, headersJSON, userAgent, userID string, isFollow bool) error {
	options := map[string]any{
		"url": fmt.Sprintf("/v3/users/%s/follow/", userID),
	}
	if isFollow {
		options["data"] = map[string]any{}
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
	form.Set("source_url", "/")
	form.Set("data", string(raw))

	endpoint := "ApiResource/update"
	if !isFollow {
		endpoint = "ApiResource/delete"
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://www.pinterest.com/resource/"+endpoint+"/", strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	applyDefaultHeaders(req, "/", userAgent, cookiesHeader, "www/[username].js")
	applyCapturedHeaders(req, headersJSON)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8")

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
