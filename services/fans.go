package services

import (
	"context"
	"errors"
	"net/http"
	"papi/parsers"
	"strings"
	"sync"
)

// FetchFollowers retrieves the list of users following the given username.
func FetchFollowers(ctx context.Context, client *http.Client, cookiesHeader, headersJSON, userAgent, username string) ([]parsers.UserMetadata, error) {
	return fetchFansList(ctx, client, cookiesHeader, username, "UserFollowersResource/get")
}

// FetchFollowing retrieves the list of users that the given username is following.
func FetchFollowing(ctx context.Context, client *http.Client, cookiesHeader, headersJSON, userAgent, username string) ([]parsers.UserMetadata, error) {
	return fetchFansList(ctx, client, cookiesHeader, username, "UserFollowingResource/get")
}

func fetchFansList(ctx context.Context, client *http.Client, cookiesHeader, username, endpoint string) ([]parsers.UserMetadata, error) {
	if username == "" {
		return nil, errors.New("username is required")
	}

	options := map[string]any{
		"username":  username,
		"page_size": 250,
	}
	sourceURL := "/" + username + "/"

	body, err := fetchResource(ctx, client, cookiesHeader, endpoint, sourceURL, "www/[username].js", options)
	if err != nil {
		return nil, err
	}

	users, err := parsers.ExtractUsersFromJSON(string(body))
	if err != nil {
		return nil, err
	}

	filtered := filterOwner(users, username)
	return resolveFullMetadataConcurrently(ctx, client, cookiesHeader, filtered), nil
}

func filterOwner(users []parsers.UserMetadata, ownerUsername string) []parsers.UserMetadata {
	var filtered []parsers.UserMetadata
	for _, u := range users {
		if strings.EqualFold(u.Username, ownerUsername) {
			continue
		}
		filtered = append(filtered, u)
	}
	return filtered
}

func resolveFullMetadataConcurrently(ctx context.Context, client *http.Client, cookiesHeader string, users []parsers.UserMetadata) []parsers.UserMetadata {
	if len(users) == 0 {
		return users
	}

	type result struct {
		index int
		meta  *parsers.UserMetadata
		err   error
	}

	ch := make(chan result, len(users))
	
	// Limit concurrency to 8 parallel requests to protect the client and Pinterest from rate limits
	sem := make(chan struct{}, 8)
	var wg sync.WaitGroup

	for i, u := range users {
		if ctx.Err() != nil {
			break
		}

		wg.Add(1)
		go func(index int, username string) {
			defer wg.Done()

			// Acquire worker slot
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-sem }()

			// Double check cancellation before network call
			if ctx.Err() != nil {
				return
			}

			meta, err := FetchUserMetadata(ctx, client, cookiesHeader, username, "/"+username+"/")
			ch <- result{index: index, meta: meta, err: err}
		}(i, u.Username)
	}

	go func() {
		wg.Wait()
		close(ch)
	}()

	resolved := make([]parsers.UserMetadata, len(users))
	copy(resolved, users)

	for res := range ch {
		if res.err == nil && res.meta != nil {
			resolved[res.index] = *res.meta
		}
	}

	return resolved
}
