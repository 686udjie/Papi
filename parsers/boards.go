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
