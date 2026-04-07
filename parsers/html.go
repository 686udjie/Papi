package parsers

import (
	"encoding/json"
	"strings"
)

func ParsePinterestHTML(html string, id string) (*Response, bool) {
	if resp, ok := ParsePinterestHTMLRelay(html, id); ok {
		return resp, true
	}
	if resp, ok := ParsePinterestHTMLStructuredData(html, id); ok {
		return resp, true
	}

	title, _ := extractMetaContent(html, "og:title")
	description, _ := extractMetaContent(html, "og:description")
	creator, _ := firstMeta(html, []string{"author", "pinterestapp:creator"})

	if videoURL, ok := firstMeta(html, []string{"og:video", "og:video:url"}); ok {
		return buildResponse(id, "video", videoURL, title, description, creator), true
	}

	if imageURL, ok := firstMeta(html, []string{"og:image", "og:image:url"}); ok {
		return buildResponse(id, "image", imageURL, title, description, creator), true
	}

	return nil, false
}

func ParsePinterestHTMLRelay(html string, id string) (*Response, bool) {
	for _, data := range extractRelayJSONObjects(html) {
		if resp, ok := parseRelayData(data, id); ok {
			return resp, true
		}
	}
	return nil, false
}

func ParsePinterestHTMLStructuredData(html string, id string) (*Response, bool) {
	for _, raw := range extractJSONLDScripts(html) {
		if raw == "" {
			continue
		}
		var data any
		if err := json.Unmarshal([]byte(raw), &data); err != nil {
			continue
		}
		if resp, ok := parseStructuredData(data, id); ok {
			return resp, true
		}
	}
	return nil, false
}

func parseStructuredData(data any, id string) (*Response, bool) {
	switch v := data.(type) {
	case map[string]any:
		if resp, ok := parseStructuredDataObject(v, id); ok {
			return resp, true
		}
	case []any:
		for _, item := range v {
			if resp, ok := parseStructuredData(item, id); ok {
				return resp, true
			}
		}
	}
	return nil, false
}

func parseStructuredDataObject(obj map[string]any, id string) (*Response, bool) {
	if objType, ok := getString(obj["@type"]); ok {
		if strings.EqualFold(objType, "SocialMediaPosting") || strings.EqualFold(objType, "ImageObject") {
			title, _ := getString(obj["headline"])
			description, _ := getString(obj["articleBody"])
			creator := ""
			if author, ok := getMap(obj["author"]); ok {
				if name, ok := getString(author["name"]); ok {
					creator = name
				}
			}

			if url, ok := extractVideoURL(obj); ok {
				return buildResponse(id, "video", url, title, description, creator), true
			}

			if imgURL, ok := extractImageURL(obj["image"]); ok {
				return buildResponse(id, "image", imgURL, title, description, creator), true
			}
		}
	}

	if graph, ok := obj["@graph"]; ok {
		return parseStructuredData(graph, id)
	}

	return nil, false
}

func parseRelayData(data map[string]any, id string) (*Response, bool) {
	root, ok := getMapPath(data, "data", "v3GetPinQueryv2", "data")
	if !ok {
		return nil, false
	}

	videoURL, ok := findBestVideoURL(root)
	if !ok {
		return nil, false
	}

	title, _ := getString(root["seoTitle"])
	description, _ := getString(root["seoDescription"])
	creator := ""
	if pinner, ok := getMap(root["pinner"]); ok {
		if name, ok := getString(pinner["fullName"]); ok {
			creator = name
		}
	}

	return buildResponse(id, "video", videoURL, title, description, creator), true
}

func extractJSONLDScripts(html string) []string {
	lower := strings.ToLower(html)
	var scripts []string
	pos := 0
	for {
		idx := strings.Index(lower[pos:], "application/ld+json")
		if idx == -1 {
			break
		}
		idx += pos

		start := strings.LastIndex(lower[:idx], "<script")
		if start == -1 {
			pos = idx + 1
			continue
		}
		startTagEnd := strings.Index(lower[start:], ">")
		if startTagEnd == -1 {
			pos = idx + 1
			continue
		}
		startTagEnd += start

		end := strings.Index(lower[startTagEnd:], "</script>")
		if end == -1 {
			pos = idx + 1
			continue
		}
		end += startTagEnd

		content := strings.TrimSpace(html[startTagEnd+1 : end])
		scripts = append(scripts, content)
		pos = end + len("</script>")
	}
	return scripts
}

func extractApplicationJSONScripts(html string) []string {
	lower := strings.ToLower(html)
	var scripts []string
	pos := 0
	for {
		idx := strings.Index(lower[pos:], `type="application/json"`)
		if idx == -1 {
			idx = strings.Index(lower[pos:], `type='application/json'`)
		}
		if idx == -1 {
			break
		}
		idx += pos

		start := strings.LastIndex(lower[:idx], "<script")
		if start == -1 {
			pos = idx + 1
			continue
		}
		startTagEnd := strings.Index(lower[start:], ">")
		if startTagEnd == -1 {
			pos = idx + 1
			continue
		}
		startTagEnd += start

		end := strings.Index(lower[startTagEnd:], "</script>")
		if end == -1 {
			pos = idx + 1
			continue
		}
		end += startTagEnd

		content := strings.TrimSpace(html[startTagEnd+1 : end])
		if content != "" {
			scripts = append(scripts, content)
		}
		pos = end + len("</script>")
	}
	return scripts
}

func extractRelayJSONObjects(html string) []map[string]any {
	const marker = "window.__PWS_RELAY_REGISTER_COMPLETED_REQUEST__"
	var results []map[string]any
	pos := 0
	for {
		idx := strings.Index(html[pos:], marker)
		if idx == -1 {
			break
		}
		idx += pos

		open := strings.Index(html[idx:], "{")
		if open == -1 {
			pos = idx + len(marker)
			continue
		}
		open += idx

		decoder := json.NewDecoder(strings.NewReader(html[open:]))
		decoder.UseNumber()
		var obj map[string]any
		if err := decoder.Decode(&obj); err == nil {
			results = append(results, obj)
		}

		pos = open + 1
	}
	return results
}

func firstMeta(html string, keys []string) (string, bool) {
	for _, key := range keys {
		if value, ok := extractMetaContent(html, key); ok {
			return value, true
		}
	}
	return "", false
}
