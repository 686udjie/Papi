package parsers

import (
	"encoding/json"
	"errors"
	"strings"
)

type UserMetadata struct {
	ID             string `json:"id"`
	Username       string `json:"username"`
	FullName       string `json:"full_name"`
	About          string `json:"about"`
	FollowerCount  int    `json:"follower_count"`
	FollowingCount int    `json:"following_count"`
	BoardCount     int    `json:"board_count"`
	PinCount       int    `json:"pin_count"`
	ImageURL       string `json:"image_xlarge_url"`
}

// ParseUserMetadataFromJSON extracts user metadata from a Pinterest Resource API response.
func ParseUserMetadataFromJSON(raw string) (*UserMetadata, error) {
	var rawData map[string]any
	if err := json.Unmarshal([]byte(raw), &rawData); err != nil {
		return nil, err
	}

	data, ok := getMapPath(rawData, "resource_response", "data")
	if !ok {
		return nil, errors.New("missing user data in response")
	}

	meta := &UserMetadata{}
	meta.ID, _ = getString(data["id"])
	meta.Username, _ = getString(data["username"])
	meta.FullName, _ = getString(data["full_name"])
	meta.About, _ = getString(data["about"])
	meta.FollowerCount, _ = getInt(data["follower_count"])
	meta.FollowingCount, _ = getInt(data["following_count"])
	meta.BoardCount, _ = getInt(data["board_count"])
	meta.PinCount, _ = getInt(data["pin_count"])
	meta.ImageURL, _ = getString(data["image_xlarge_url"])

	// Align counts with UI:
	// 1. Subtract hidden "Quick Save" (Unorganized ideas) board if present
	if hasQuickSave, _ := data["has_quicksave_board"].(bool); hasQuickSave && meta.BoardCount > 0 {
		meta.BoardCount--
	}
	// 2. Use explicit following count (actual users followed) if available
	if explicit, ok := getInt(data["explicit_user_following_count"]); ok {
		meta.FollowingCount = explicit
	}

	if meta.ID == "" {
		return nil, errors.New("user id not found in metadata")
	}

	return meta, nil
}

// ExtractUserMetadataFromHTML finds and parses the UserResource JSON embedded in a Pinterest profile page.
func ExtractUserMetadataFromHTML(html string) (*UserMetadata, error) {
	raw, err := ExtractResourceJSON(html, "UserResource")
	if err != nil {
		return nil, err
	}
	return ParseUserMetadataFromJSON(raw)
}

// ExtractUsername parses a Pinterest profile URL to extract the username.
func ExtractUsername(rawURL string) string {
	rawURL = strings.TrimPrefix(rawURL, "https://")
	rawURL = strings.TrimPrefix(rawURL, "http://")
	rawURL = strings.TrimPrefix(rawURL, "www.pinterest.com")
	rawURL = strings.TrimPrefix(rawURL, "pinterest.com")
	
	parts := strings.Split(strings.Trim(rawURL, "/"), "/")
	if len(parts) > 0 {
		return parts[0]
	}
	return ""
}
