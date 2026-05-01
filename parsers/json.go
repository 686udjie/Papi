package parsers

import (
	"encoding/json"
	"errors"
	"regexp"
	"strings"
)

var ErrJSONNotFound = errors.New("json not found")

func ExtractJSON(html string) (string, error) {
	re := regexp.MustCompile(`(?s)<script id="__PWS_DATA__" type="application/json">\s*(.*?)\s*</script>`)
	match := re.FindStringSubmatch(html)
	if len(match) < 2 {
		return "", ErrJSONNotFound
	}
	return strings.TrimSpace(match[1]), nil
}

func ExtractResourceJSON(html string, resourceName string) (string, error) {
	scripts := extractApplicationJSONScripts(html)
	for _, script := range scripts {
		if strings.Contains(script, `"name":"`+resourceName+`"`) {
			return script, nil
		}
	}
	return "", ErrJSONNotFound
}

func ParsePinterestJSON(raw string, id string) (*Response, error) {
	var data map[string]any
	if err := json.Unmarshal([]byte(raw), &data); err != nil {
		return nil, err
	}

	pin, ok := findPin(data, id)
	if !ok {
		return nil, errors.New("pin not found")
	}

	resp := &Response{ID: id}
	setMetadata(resp, pin)

	if url, ok := bestVideoURL(pin); ok {
		return buildResponse(id, "video", url, resp.Title, resp.Description, resp.Creator), nil
	}

	if imageURL, width, height, ok := bestImage(pin); ok {
		image := buildResponse(id, "image", imageURL, resp.Title, resp.Description, resp.Creator)
		image.Width = width
		image.Height = height
		return image, nil
	}

	return nil, errors.New("no media found")
}

func findPin(data map[string]any, id string) (map[string]any, bool) {
	paths := [][]string{
		{"props", "pageProps", "initialReduxState", "pins"},
		{"pageProps", "initialReduxState", "pins"},
		{"props", "initialReduxState", "pins"},
		{"initialReduxState", "pins"},
	}
	for _, path := range paths {
		if pins, ok := getMapPath(data, path...); ok {
			if pin, ok := getMap(pins[id]); ok {
				return pin, true
			}
		}
	}

	if cache, ok := data["resourceDataCache"].([]any); ok {
		for _, item := range cache {
			entry, ok := getMap(item)
			if !ok {
				continue
			}
			dataMap, ok := getMap(entry["data"])
			if !ok {
				continue
			}
			if pid, ok := getString(dataMap["id"]); ok && pid == id {
				return dataMap, true
			}
		}
	}

	return nil, false
}

func setMetadata(resp *Response, pin map[string]any) {
	if title, ok := getString(pin["title"]); ok {
		resp.Title = title
	}
	if desc, ok := getString(pin["description"]); ok {
		resp.Description = desc
	}

	if pinner, ok := getMap(pin["pinner"]); ok {
		if name, ok := getString(pinner["full_name"]); ok {
			resp.Creator = name
			return
		}
		if name, ok := getString(pinner["username"]); ok {
			resp.Creator = name
			return
		}
	}
}

func bestVideoURL(pin map[string]any) (string, bool) {
	videos, ok := getMap(pin["videos"])
	if !ok {
		return "", false
	}
	videoList, ok := getMap(videos["video_list"])
	if !ok {
		return "", false
	}

	for _, v := range videoList {
		video, ok := getMap(v)
		if !ok {
			continue
		}
		url, ok := getString(video["url"])
		if !ok {
			continue
		}
		if strings.HasSuffix(strings.ToLower(url), ".mp4") {
			return url, true
		}
	}
	return "", false
}

func bestImage(pin map[string]any) (string, int, int, bool) {
	images, ok := getMap(pin["images"])
	if !ok {
		return "", 0, 0, false
	}
	orig, ok := getMap(images["orig"])
	if !ok {
		return "", 0, 0, false
	}

	url, ok := getString(orig["url"])
	if !ok {
		return "", 0, 0, false
	}
	width, _ := getInt(orig["width"])
	height, _ := getInt(orig["height"])

	return url, width, height, true
}
