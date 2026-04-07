package parsers

import (
	"encoding/json"
	"errors"
)

var ErrSearchPinsNotFound = errors.New("search pins not found")

func ExtractSearchPinsFromHTML(html string) ([]map[string]any, error) {
	collector := &searchPinCollector{
		seen: make(map[string]struct{}),
	}

	if raw, err := ExtractJSON(html); err == nil {
		collector.collectJSON(raw)
	}

	for _, raw := range extractApplicationJSONScripts(html) {
		collector.collectJSON(raw)
	}

	if len(collector.pins) == 0 {
		return nil, ErrSearchPinsNotFound
	}
	return collector.pins, nil
}

func ExtractSearchPinsFromJSON(raw string) ([]map[string]any, error) {
	collector := &searchPinCollector{
		seen: make(map[string]struct{}),
	}
	if err := collector.collectJSON(raw); err != nil {
		return nil, err
	}

	if len(collector.pins) == 0 {
		return nil, ErrSearchPinsNotFound
	}
	return collector.pins, nil
}

type searchPinCollector struct {
	pins []map[string]any
	seen map[string]struct{}
}

func (c *searchPinCollector) collectJSON(raw string) error {
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

func (c *searchPinCollector) walk(value any, parentKey string) {
	switch v := value.(type) {
	case map[string]any:
		if parentKey == "pins" {
			for _, item := range v {
				if pin, ok := getMap(item); ok {
					c.add(pin)
				}
			}
		}

		if isPinObject(v) {
			c.add(v)
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

func (c *searchPinCollector) add(pin map[string]any) {
	id, ok := getString(pin["id"])
	if !ok {
		return
	}
	if _, exists := c.seen[id]; exists {
		return
	}
	c.seen[id] = struct{}{}
	c.pins = append(c.pins, pin)
}

func isPinObject(obj map[string]any) bool {
	id, ok := getString(obj["id"])
	if !ok || id == "" {
		return false
	}

	if t, ok := getString(obj["type"]); ok && t == "pin" {
		return true
	}

	_, hasImages := getMap(obj["images"])
	_, hasVideos := getMap(obj["videos"])
	_, hasStoryPinData := getMap(obj["story_pin_data"])
	_, hasPinJoin := getMap(obj["pin_join"])
	_, hasAggregatedPinData := getMap(obj["aggregated_pin_data"])

	if hasImages || hasVideos || hasStoryPinData || hasPinJoin || hasAggregatedPinData {
		return true
	}

	if _, ok := getString(obj["grid_title"]); ok {
		return true
	}
	if _, ok := getString(obj["link"]); ok {
		return true
	}
	if _, ok := getString(obj["dominant_color"]); ok {
		return true
	}
	if _, ok := getString(obj["image_signature"]); ok {
		return true
	}

	return false
}
