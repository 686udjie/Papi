package parsers

import (
	"encoding/json"
	"errors"
	"strings"
)

var ErrBoardSectionsNotFound = errors.New("board sections not found")

type BoardSection struct {
	ID    string `json:"id"`
	Slug  string `json:"slug"`
	Title string `json:"title"`
}
 
type BoardMetadata struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	URL          string   `json:"url"`
	Username     string   `json:"username"`
	Slug         string   `json:"slug"`
	SectionCount int      `json:"section_count"`
	PinCount     int      `json:"pin_count"`
	CoverURLs    []string `json:"cover_urls"`
}

func ExtractBoardMetadataFromHTML(html string) (*BoardMetadata, error) {
	raw, err := ExtractResourceJSON(html, "BoardResource")
	if err != nil {
		return nil, err
	}

	var wrapper struct {
		ResourceResponse struct {
			Data BoardMetadata `json:"data"`
		} `json:"resource_response"`
	}

	if err := json.Unmarshal([]byte(raw), &wrapper); err != nil {
		return nil, err
	}

	if wrapper.ResourceResponse.Data.ID == "" {
		return nil, errors.New("board id not found in metadata")
	}

	return &wrapper.ResourceResponse.Data, nil
}

func ExtractBoardSectionsFromHTML(html string) ([]BoardSection, error) {
	collector := &boardSectionCollector{
		seen: make(map[string]struct{}),
	}

	if raw, err := ExtractJSON(html); err == nil {
		_ = collector.collectJSON(raw)
	}

	for _, raw := range extractApplicationJSONScripts(html) {
		_ = collector.collectJSON(raw)
	}

	if len(collector.sections) == 0 {
		return nil, ErrBoardSectionsNotFound
	}
	return collector.sections, nil
}

func ExtractBoardSectionsFromJSON(raw string) ([]BoardSection, error) {
	collector := &boardSectionCollector{
		seen: make(map[string]struct{}),
	}
	if err := collector.collectJSON(raw); err != nil {
		return nil, err
	}
	if len(collector.sections) == 0 {
		return nil, ErrBoardSectionsNotFound
	}
	return collector.sections, nil
}

func ExtractBoardsFromJSON(raw string) ([]BoardMetadata, error) {
	collector := &boardCollector{
		seen:   make(map[string]struct{}),
		boards: make([]BoardMetadata, 0),
	}
	if err := collector.collectJSON(raw); err != nil {
		return nil, err
	}
	return collector.boards, nil
}

func ExtractBoardsFromHTML(html string) ([]BoardMetadata, error) {
	collector := &boardCollector{
		seen:   make(map[string]struct{}),
		boards: make([]BoardMetadata, 0),
	}
	for _, script := range extractApplicationJSONScripts(html) {
		_ = collector.collectJSON(script)
	}
	if len(collector.boards) == 0 {
		return nil, errors.New("no boards found in html")
	}
	return collector.boards, nil
}

type boardCollector struct {
	boards []BoardMetadata
	seen   map[string]struct{}
}

func (c *boardCollector) collectJSON(raw string) error {
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

func (c *boardCollector) walk(value any) {
	switch v := value.(type) {
	case map[string]any:
		if board, ok := parseBoardMetadata(v); ok {
			c.add(board)
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

func (c *boardCollector) add(board BoardMetadata) {
	if board.ID == "" {
		return
	}
	if _, exists := c.seen[board.ID]; exists {
		return
	}
	c.seen[board.ID] = struct{}{}
	c.boards = append(c.boards, board)
}

func parseBoardMetadata(obj map[string]any) (BoardMetadata, bool) {
	id, _ := getString(obj["id"])
	name, _ := getString(obj["name"])
	url, _ := getString(obj["url"])
	if id == "" || name == "" || url == "" {
		return BoardMetadata{}, false
	}

	objType, _ := getString(obj["type"])
	if objType != "board" && !strings.Contains(url, "/") {
		return BoardMetadata{}, false
	}

	sectionCount, _ := getInt(obj["section_count"])
	pinCount, _ := getInt(obj["pin_count"])

	owner, _ := getMap(obj["owner"])
	username, _ := getString(owner["username"])
	if username == "" {
		username, _ = getString(obj["username"])
	}

	slug := ""
	trimmed := strings.Trim(url, "/")
	parts := strings.Split(trimmed, "/")
	if len(parts) >= 2 {
		if username == "" {
			username = parts[0]
		}
		slug = parts[1]
	}

	coverURLs := extractCoverURLs(obj)

	return BoardMetadata{
		ID:           id,
		Name:         name,
		URL:          url,
		Username:     username,
		Slug:         slug,
		SectionCount: sectionCount,
		PinCount:     pinCount,
		CoverURLs:    coverURLs,
	}, true
}

func extractCoverURLs(obj map[string]any) []string {
	// A. Find the main cover image
	var mainCover string

	if s, ok := getString(obj["image_cover_url"]); ok {
		mainCover = s
	}
	if mainCover == "" {
		if media, ok := getMap(obj["media"]); ok {
			if s, ok := getString(media["image_cover_url"]); ok {
				mainCover = s
			}
		}
	}
	if mainCover == "" {
		if val, ok := obj["cover_images"]; ok {
			if arr, ok := val.([]any); ok && len(arr) > 0 {
				if m, ok := getMap(arr[0]); ok {
					if s, ok := getString(m["url"]); ok {
						mainCover = s
					}
				} else if s, ok := arr[0].(string); ok {
					mainCover = s
				}
			}
		}
	}

	// B. Find the preview pin thumbnails
	var previewURLs []string
	var rawPreviews []any
	if val, ok := obj["pin_thumbnail_urls"]; ok {
		if arr, ok := val.([]any); ok {
			rawPreviews = arr
		}
	}
	if len(rawPreviews) == 0 {
		if media, ok := getMap(obj["media"]); ok {
			if val, ok := media["pin_thumbnail_urls"]; ok {
				if arr, ok := val.([]any); ok {
					rawPreviews = arr
				}
			}
		}
	}
	for _, item := range rawPreviews {
		if s, ok := item.(string); ok && s != "" {
			previewURLs = append(previewURLs, s)
		}
	}

	// C. Assemble the final list: mainCover first, followed by previews
	var urls []string
	if mainCover != "" {
		urls = append(urls, mainCover)
	}

	for _, u := range previewURLs {
		urls = append(urls, u)
	}

	seen := make(map[string]bool)
	var finalUrls []string
	for _, u := range urls {
		u = strings.TrimSpace(u)
		if u != "" && !seen[u] {
			seen[u] = true
			finalUrls = append(finalUrls, u)
		}
	}

	return finalUrls
}

// ExtractPinImageURL extracts the cleanest image URL from an individual Pin map.
func ExtractPinImageURL(pin map[string]any) string {
	images, ok := getMap(pin["images"])
	if !ok {
		return ""
	}
	// Try multiple resolutions
	for _, size := range []string{"orig", "736x", "564x", "474x", "236x", "170x"} {
		if img, ok := getMap(images[size]); ok {
			if url, ok := getString(img["url"]); ok && url != "" {
				return url
			}
		}
	}
	return ""
}

type boardSectionCollector struct {
	sections []BoardSection
	seen     map[string]struct{}
}

func (c *boardSectionCollector) collectJSON(raw string) error {
	if raw == "" {
		return nil
	}
	var data any
	if err := json.Unmarshal([]byte(raw), &data); err != nil {
		return err
	}
	c.walk(data, "")
	return nil
}

func (c *boardSectionCollector) walk(value any, parentKey string) {
	switch v := value.(type) {
	case map[string]any:
		if section, ok := parseBoardSection(v, parentKey); ok {
			c.add(section)
		}
		for key, item := range v {
			c.walk(item, key)
		}
	case []any:
		for _, item := range v {
			c.walk(item, parentKey)
		}
	}
}

func (c *boardSectionCollector) add(section BoardSection) {
	if section.ID == "" || section.Slug == "" {
		return
	}
	if _, exists := c.seen[section.ID]; exists {
		return
	}
	c.seen[section.ID] = struct{}{}
	c.sections = append(c.sections, section)
}

func parseBoardSection(obj map[string]any, parentKey string) (BoardSection, bool) {
	id := firstString(obj, "section_id", "board_section_id", "id")
	slug := firstString(obj, "section_slug", "slug")
	if id == "" || slug == "" {
		return BoardSection{}, false
	}

	objType := strings.ToLower(firstString(obj, "type"))
	isSectionParent := parentKey == "sections" || parentKey == "board_sections" || strings.Contains(parentKey, "section")
	isSectionType := strings.Contains(objType, "board_section") || objType == "section"
	_, hasSectionCover := getMap(obj["section_cover_image"])
	hasBoardHints := firstString(obj, "board_id") != "" || firstString(obj, "pin_count") != "" || hasSectionCover || firstString(obj, "title", "name") != ""
	if !isSectionParent && !isSectionType && !hasBoardHints {
		return BoardSection{}, false
	}

	title := firstString(obj, "title", "name")
	if title == "" {
		title = slug
	}

	return BoardSection{
		ID:    id,
		Slug:  slug,
		Title: title,
	}, true
}

func firstString(obj map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := getString(obj[key]); ok {
			return value
		}
	}
	return ""
}
