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

// ExtractUsersFromJSON extracts a list of users from a JSON response.
func ExtractUsersFromJSON(raw string) ([]UserMetadata, error) {
	collector := &userCollector{
		seen:  make(map[string]struct{}),
		users: make([]UserMetadata, 0),
	}
	if err := collector.collectJSON(raw); err != nil {
		return nil, err
	}
	return collector.users, nil
}

func ExtractUsersFromHTML(html string) ([]UserMetadata, error) {
	collector := &userCollector{
		seen:  make(map[string]struct{}),
		users: make([]UserMetadata, 0),
	}
	for _, script := range extractApplicationJSONScripts(html) {
		_ = collector.collectJSON(script)
	}
	if len(collector.users) == 0 {
		return nil, errors.New("no users found in html")
	}
	return collector.users, nil
}


type userCollector struct {
	users []UserMetadata
	seen  map[string]struct{}
}

func (c *userCollector) collectJSON(raw string) error {
	if raw == "" {
		return nil
	}
	var data any
	if err := json.Unmarshal([]byte(raw), &data); err != nil {
		return err
	}
	c.walk(data)
	return nil
}

func (c *userCollector) walk(value any) {
	switch v := value.(type) {
	case map[string]any:
		if user, ok := parseUserMetadata(v); ok {
			c.add(user)
		}
		for _, item := range v {
			c.walk(item)
		}
	case []any:
		for _, item := range v {
			c.walk(item)
		}
	}
}

func (c *userCollector) add(user UserMetadata) {
	if user.ID == "" {
		return
	}
	if _, exists := c.seen[user.ID]; exists {
		return
	}
	c.seen[user.ID] = struct{}{}
	c.users = append(c.users, user)
}

func parseUserMetadata(obj map[string]any) (UserMetadata, bool) {
	id, _ := getString(obj["id"])
	username, _ := getString(obj["username"])
	if id == "" || username == "" {
		return UserMetadata{}, false
	}

	objType, _ := getString(obj["type"])
	if objType != "user" && objType != "profile" {
		// Heuristic: check for fields typical of a user object if type is missing
		if _, ok := obj["full_name"]; !ok {
			return UserMetadata{}, false
		}
	}

	meta := UserMetadata{
		ID:       id,
		Username: username,
	}
	meta.FullName, _ = getString(obj["full_name"])
	meta.About, _ = getString(obj["about"])
	meta.FollowerCount, _ = getInt(obj["follower_count"])
	meta.FollowingCount, _ = getInt(obj["following_count"])
	meta.BoardCount, _ = getInt(obj["board_count"])
	meta.PinCount, _ = getInt(obj["pin_count"])
	meta.ImageURL, _ = getString(obj["image_xlarge_url"])
	if meta.ImageURL == "" {
		meta.ImageURL, _ = getString(obj["image_medium_url"])
	}
	if meta.ImageURL == "" {
		meta.ImageURL, _ = getString(obj["image_small_url"])
	}

	return meta, true
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
