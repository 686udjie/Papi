package services

import (
	"context"
	"errors"
	"net/http"
	"papi/parsers"
)

type UserResponse struct {
	Metadata *parsers.UserMetadata    `json:"metadata"`
	Boards   []parsers.BoardMetadata `json:"boards"`
}

// FetchUser orchestrates fetching both user metadata and their collection of boards.
func FetchUser(ctx context.Context, client *http.Client, cookiesHeader, profileURL string) (*UserResponse, error) {
	username := parsers.ExtractUsername(profileURL)
	if username == "" {
		return nil, errors.New("invalid profile url")
	}

	sourcePath := "/" + username + "/"
	metadata, err := FetchUserMetadata(ctx, client, cookiesHeader, username, sourcePath)
	if err != nil {
		return nil, err
	}

	// Boards fetch is resilient; returns empty slice on failure instead of breaking the entire response.
	boards, err := FetchUserBoards(ctx, client, cookiesHeader, metadata.Username, sourcePath)
	if err != nil {
		boards = []parsers.BoardMetadata{}
	}

	return &UserResponse{
		Metadata: metadata,
		Boards:   boards,
	}, nil
}

// FetchUserMetadata retrieves core profile information via UserResource.
func FetchUserMetadata(ctx context.Context, client *http.Client, cookiesHeader, username, sourceURL string) (*parsers.UserMetadata, error) {
	options := map[string]any{"username": username}
	body, err := fetchResource(ctx, client, cookiesHeader, "UserResource/get", sourceURL, "www/[username].js", options)
	if err != nil {
		return nil, err
	}
	return parsers.ParseUserMetadataFromJSON(string(body))
}

// FetchUserBoards retrieves the list of boards for a user via BoardsResource.
func FetchUserBoards(ctx context.Context, client *http.Client, cookiesHeader, username, sourceURL string) ([]parsers.BoardMetadata, error) {
	options := map[string]any{
		"username":             username,
		"page_size":            25,
		"privacy_filter":       "all",
		"sort":                 "last_pinned_to",
		"field_set_key":        "profile_grid_item",
		"group_by":             "mix_public_private",
		"include_archived":     true,
		"redux_normalize_feed": true,
	}

	body, err := fetchResource(ctx, client, cookiesHeader, "BoardsResource/get", sourceURL, "www/[username].js", options)
	if err != nil {
		return nil, err
	}

	return parsers.ExtractBoardsFromJSON(string(body))
}
