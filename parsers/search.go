package parsers

import (
	"encoding/json"
	"errors"
	"strings"
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

func isPromotedPin(pin map[string]any) bool {
	// Direct promotion flags
	if checkBoolField(pin, "is_promoted") ||
	   checkBoolField(pin, "is_downstream_promotion") ||
	   checkBoolField(pin, "promoted_is_lead_ad") ||
	   checkBoolField(pin, "promoted_is_removable") ||
	   checkBoolField(pin, "is_ads_only_profile") {
		return true
	}
	
	// Object fields indicating promotion
	if hasObjectField(pin, "promoted_pin") ||
	   hasObjectField(pin, "sponsor") ||
	   hasObjectField(pin, "advertiser") {
		return true
	}
	
	// String fields indicating promotion
	if checkNonEmptyStringField(pin, "promoted_type") {
		return true
	}
	
	// Numeric fields indicating ads
	if adMatchReason, ok := getInt(pin["ad_match_reason"]); ok && adMatchReason > 0 {
		return true
	}
	
	// Commercial content indicators
	if hasArrayField(pin, "shopping_flags") ||
	   hasProductMetadata(pin) {
		return true
	}
	
	// Link-based promotion indicators
	if hasAffiliateLinks(pin) || hasPromotionalTracking(pin) {
		return true
	}
	
	return false
}

func checkBoolField(pin map[string]any, field string) bool {
	if val, ok := getBool(pin[field]); ok {
		return val
	}
	return false
}

func hasObjectField(pin map[string]any, field string) bool {
	_, ok := getMap(pin[field])
	return ok
}

func checkNonEmptyStringField(pin map[string]any, field string) bool {
	if val, ok := getString(pin[field]); ok {
		return val != ""
	}
	return false
}

func hasArrayField(pin map[string]any, field string) bool {
	if val, ok := pin[field].([]any); ok {
		return len(val) > 0
	}
	return false
}

func hasProductMetadata(pin map[string]any) bool {
	if richSummary, ok := getMap(pin["rich_summary"]); ok {
		if products, ok := richSummary["products"].([]any); ok {
			return len(products) > 0
		}
	}
	return false
}

func hasAffiliateLinks(pin map[string]any) bool {
	if utmLink, ok := getString(pin["utm_link"]); ok && utmLink != "" {
		utmLink = strings.ToLower(utmLink)
		return strings.Contains(utmLink, "skimresources") ||
			   strings.Contains(utmLink, "affiliate") ||
			   strings.Contains(utmLink, "referral")
	}
	return false
}

func hasPromotionalTracking(pin map[string]any) bool {
	if trackingParams, ok := getString(pin["tracking_params"]); ok {
		trackingParams = strings.ToLower(trackingParams)
		return strings.Contains(trackingParams, "promoted") ||
			   strings.Contains(trackingParams, "sponsor") ||
			   strings.Contains(trackingParams, "advertiser")
	}
	return false
}

func FilterHomefeedPins(data map[string]any) {
	// Navigate to the pins array in homefeed response
	if resourceResponse, ok := data["resource_response"].(map[string]any); ok {
		if responseData, ok := resourceResponse["data"].(map[string]any); ok {
			if results, ok := responseData["results"].([]any); ok {
				// Filter out promoted pins from results
				filteredResults := make([]any, 0, len(results))
				for _, item := range results {
					if pin, ok := item.(map[string]any); ok {
						if !isPromotedPin(pin) {
							filteredResults = append(filteredResults, pin)
						}
					} else {
						// Keep non-pin items as is
						filteredResults = append(filteredResults, item)
					}
				}
				responseData["results"] = filteredResults
			}
		}
	}
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
