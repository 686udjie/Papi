package parsers

import (
	"net/url"
	"path"
	"regexp"
	"strings"
)

func getMap(v any) (map[string]any, bool) {
	m, ok := v.(map[string]any)
	return m, ok
}

func getMapPath(root map[string]any, keys ...string) (map[string]any, bool) {
	current := root
	for i, key := range keys {
		value, ok := current[key]
		if !ok {
			return nil, false
		}
		if i == len(keys)-1 {
			m, ok := value.(map[string]any)
			return m, ok
		}
		next, ok := value.(map[string]any)
		if !ok {
			return nil, false
		}
		current = next
	}
	return nil, false
}

func getString(v any) (string, bool) {
	s, ok := v.(string)
	if !ok {
		return "", false
	}
	s = strings.TrimSpace(s)
	if s == "" {
		return "", false
	}
	return s, true
}

func getFloat64(v any) (float64, bool) {
	f, ok := v.(float64)
	return f, ok
}

func getInt(v any) (int, bool) {
	f, ok := getFloat64(v)
	if !ok {
		return 0, false
	}
	return int(f), true
}

func extractMetaContent(html string, property string) (string, bool) {
	key := regexp.QuoteMeta(property)
	re1 := regexp.MustCompile(`(?i)<meta[^>]+(?:property|name)=["']` + key + `["'][^>]+content=["']([^"']+)["']`)
	if match := re1.FindStringSubmatch(html); len(match) > 1 {
		value := strings.TrimSpace(match[1])
		if value != "" {
			return value, true
		}
	}

	re2 := regexp.MustCompile(`(?i)<meta[^>]+content=["']([^"']+)["'][^>]+(?:property|name)=["']` + key + `["']`)
	if match := re2.FindStringSubmatch(html); len(match) > 1 {
		value := strings.TrimSpace(match[1])
		if value != "" {
			return value, true
		}
	}

	return "", false
}

func extFromURL(raw string, fallback string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Path == "" {
		return fallback
	}
	ext := path.Ext(u.Path)
	if ext == "" {
		return fallback
	}
	return ext
}

func extractImageURL(value any) (string, bool) {
	switch v := value.(type) {
	case string:
		return v, true
	case []any:
		for _, item := range v {
			if s, ok := item.(string); ok {
				return s, true
			}
			if m, ok := item.(map[string]any); ok {
				if s, ok := getString(m["url"]); ok {
					return s, true
				}
			}
		}
	case map[string]any:
		if s, ok := getString(v["url"]); ok {
			return s, true
		}
	}
	return "", false
}

func extractVideoURL(obj map[string]any) (string, bool) {
	if url, ok := getString(obj["contentUrl"]); ok && isVideoURL(url) {
		return url, true
	}
	if url, ok := getString(obj["embedUrl"]); ok && isVideoURL(url) {
		return url, true
	}
	if url, ok := getString(obj["url"]); ok && isVideoURL(url) {
		return url, true
	}

	switch v := obj["video"].(type) {
	case string:
		if isVideoURL(v) {
			return v, true
		}
	case map[string]any:
		if t, ok := getString(v["@type"]); ok && strings.EqualFold(t, "VideoObject") {
			if url, ok := getString(v["contentUrl"]); ok {
				return url, true
			}
			if url, ok := getString(v["url"]); ok {
				return url, true
			}
		}
		if url, ok := getString(v["contentUrl"]); ok && isVideoURL(url) {
			return url, true
		}
		if url, ok := getString(v["url"]); ok && isVideoURL(url) {
			return url, true
		}
	case []any:
		for _, item := range v {
			if s, ok := item.(string); ok && isVideoURL(s) {
				return s, true
			}
			if m, ok := item.(map[string]any); ok {
				if url, ok := getString(m["contentUrl"]); ok && isVideoURL(url) {
					return url, true
				}
				if url, ok := getString(m["url"]); ok && isVideoURL(url) {
					return url, true
				}
			}
		}
	}

	return "", false
}

func isVideoURL(u string) bool {
	lower := strings.ToLower(u)
	if strings.Contains(lower, "/videos/thumbnails/") {
		return false
	}
	if strings.HasSuffix(lower, ".jpg") ||
		strings.HasSuffix(lower, ".jpeg") ||
		strings.HasSuffix(lower, ".png") ||
		strings.HasSuffix(lower, ".webp") ||
		strings.HasSuffix(lower, ".gif") {
		return false
	}
	switch {
	case strings.HasSuffix(lower, ".mp4"),
		strings.HasSuffix(lower, ".m3u8"),
		strings.HasSuffix(lower, ".webm"):
		return true
	case strings.Contains(lower, "/videos/"):
		return true
	}
	return false
}

func buildResponse(id string, mediaType string, url string, title string, description string, creator string) *Response {
	lowerURL := strings.ToLower(url)
	if strings.HasSuffix(lowerURL, ".gif") {
		mediaType = "gif"
	}

	fallback := ".jpg"
	switch mediaType {
	case "video":
		fallback = ".mp4"
	case "gif":
		fallback = ".gif"
	}
	return &Response{
		ID:          id,
		Type:        mediaType,
		URL:         url,
		Filename:    "pinterest_" + id + extFromURL(url, fallback),
		Title:       title,
		Description: description,
		Creator:     creator,
	}
}

func findBestVideoURL(value any) (string, bool) {
	var candidates []string
	collectVideoURLs(value, &candidates)
	if len(candidates) == 0 {
		return "", false
	}
	best := candidates[0]
	bestRank := videoRank(best)
	for _, url := range candidates[1:] {
		rank := videoRank(url)
		if rank > bestRank {
			best = url
			bestRank = rank
		}
	}
	return best, true
}

func collectVideoURLs(value any, out *[]string) {
	switch v := value.(type) {
	case string:
		if isVideoURL(v) {
			*out = append(*out, v)
		}
	case map[string]any:
		for _, val := range v {
			collectVideoURLs(val, out)
		}
	case []any:
		for _, item := range v {
			collectVideoURLs(item, out)
		}
	}
}

func videoRank(u string) int {
	lower := strings.ToLower(u)
	switch {
	case strings.HasSuffix(lower, ".mp4"):
		return 3
	case strings.HasSuffix(lower, ".m3u8"):
		return 2
	case strings.HasSuffix(lower, ".webm"):
		return 1
	default:
		return 0
	}
}
