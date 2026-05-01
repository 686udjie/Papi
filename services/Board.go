package services

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"papi/parsers"
)

var (
	ErrMissingBoardURL = errors.New("missing board url")
	ErrInvalidBoardURL = errors.New("invalid board url")
)

const (
	defaultBoardPinsHandler = "www/[username]/[slug]/[section_slug].js"
	defaultBoardPinsPath    = "/resource/BoardSectionPinsResource/get/"
	defaultBoardParentPath  = "/resource/ActiveUserResource/create/"
	defaultBoardPageHandler = "www/[username]/[slug].js"
)

type BoardResponse struct {
	Username     string               `json:"username"`
	Slug         string               `json:"slug"`
	URL          string               `json:"url"`
	SectionCount int                  `json:"section_count"`
	Sections     []BoardSectionResult `json:"sections"`
}

type BoardSectionResult struct {
	ID    string           `json:"id"`
	Slug  string           `json:"slug"`
	Title string           `json:"title"`
	URL   string           `json:"url"`
	Count int              `json:"count"`
	Pins  []map[string]any `json:"pins,omitempty"`
	Error string           `json:"error,omitempty"`
}

type BoardRef struct {
	Username  string
	Slug      string
	SourceURL string
}

// ParseBoardRef extracts username and slug from a Pinterest board URL.
func ParseBoardRef(raw string) (*BoardRef, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, ErrMissingBoardURL
	}

	if strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") {
		parsed, err := url.Parse(raw)
		if err != nil {
			return nil, ErrInvalidBoardURL
		}
		raw = parsed.Path
	}

	trimmed := strings.Trim(raw, "/")
	parts := strings.Split(trimmed, "/")
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return nil, ErrInvalidBoardURL
	}

	return &BoardRef{
		Username:  parts[0],
		Slug:      parts[1],
		SourceURL: "/" + parts[0] + "/" + parts[1] + "/",
	}, nil
}

// FetchBoard retrieves board metadata and all its sections/pins.
func FetchBoard(ctx context.Context, client *http.Client, cookiesHeader, headersJSON, userAgent, rawURL string) (*BoardResponse, int, error) {
	if client == nil {
		client = http.DefaultClient
	}

	ref, err := ParseBoardRef(rawURL)
	if err != nil {
		return nil, 0, err
	}

	pageBody, status, err := FetchBoardPage(ctx, client, cookiesHeader, headersJSON, userAgent, ref.SourceURL)
	if err != nil {
		return nil, status, err
	}

	metadata, _ := parsers.ExtractBoardMetadataFromHTML(string(pageBody))
	sections, err := discoverBoardSections(ctx, client, cookiesHeader, headersJSON, userAgent, ref.SourceURL, pageBody)
	if err != nil && !errors.Is(err, parsers.ErrBoardSectionsNotFound) {
		return nil, status, err
	}

	result := &BoardResponse{
		Username: ref.Username,
		Slug:     ref.Slug,
		URL:      HomefeedBaseURL + ref.SourceURL,
		Sections: make([]BoardSectionResult, 0),
	}

	// If no sections, fall back to main board feed
	if len(sections) == 0 && metadata != nil {
		if body, s, e := FetchBoardFeedPins(ctx, client, cookiesHeader, headersJSON, userAgent, ref.SourceURL, metadata.ID); e == nil && s == 200 {
			if pins, pe := parsers.ExtractSearchPinsFromJSON(string(body)); pe == nil {
				result.Sections = append(result.Sections, BoardSectionResult{
					ID:    metadata.ID,
					Slug:  "pins",
					Title: "Pins",
					URL:   result.URL,
					Pins:  pins,
					Count: len(pins),
				})
			}
		}
	}

	for _, section := range sections {
		sectionURL := ref.SourceURL + strings.Trim(section.Slug, "/") + "/"
		sectionResult := BoardSectionResult{
			ID:    section.ID,
			Slug:  section.Slug,
			Title: section.Title,
			URL:   HomefeedBaseURL + sectionURL,
		}

		body, s, e := FetchBoardSectionPins(ctx, client, cookiesHeader, headersJSON, userAgent, sectionURL, section.ID)
		if e != nil {
			sectionResult.Error = e.Error()
		} else if s < 200 || s >= 300 {
			sectionResult.Error = "upstream returned status " + http.StatusText(s)
		} else if pins, pe := parsers.ExtractSearchPinsFromJSON(string(body)); pe != nil {
			sectionResult.Error = pe.Error()
		} else {
			sectionResult.Pins = pins
			sectionResult.Count = len(pins)
		}
		result.Sections = append(result.Sections, sectionResult)
	}

	result.SectionCount = len(result.Sections)
	return result, status, nil
}

func discoverBoardSections(ctx context.Context, client *http.Client, cookiesHeader, headersJSON, userAgent, sourceURL string, pageBody []byte) ([]parsers.BoardSection, error) {
	if sections, err := parsers.ExtractBoardSectionsFromHTML(string(pageBody)); err == nil {
		return sections, nil
	}

	parentBody, _, err := FetchBoardParentResource(ctx, client, cookiesHeader, headersJSON, userAgent, sourceURL)
	if err != nil {
		return nil, err
	}
	return parsers.ExtractBoardSectionsFromJSON(string(parentBody))
}

func FetchBoardParentResource(ctx context.Context, client *http.Client, cookiesHeader, headersJSON, userAgent, sourceURL string) ([]byte, int, error) {
	payload := map[string]any{
		"options": map[string]any{
			"data": map[string]any{
				"time":           nowFunc().UnixNano(),
				"view_type":      5,
				"view_parameter": 3070,
			},
		},
		"context": map[string]any{},
	}

	raw, _ := json.Marshal(payload)
	form := url.Values{}
	form.Set("source_url", sourceURL)
	form.Set("data", string(raw))

	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, HomefeedBaseURL+defaultBoardParentPath, strings.NewReader(form.Encode()))
	applyCapturedHeaders(req, headersJSON)
	applyDefaultHeaders(req, sourceURL, userAgent, cookiesHeader, defaultBoardPageHandler)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	return body, resp.StatusCode, nil
}

func FetchBoardPage(ctx context.Context, client *http.Client, cookiesHeader, headersJSON, userAgent, sourceURL string) ([]byte, int, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, HomefeedBaseURL+sourceURL, nil)
	applyCapturedHeaders(req, headersJSON)
	applyDefaultHeaders(req, sourceURL, userAgent, cookiesHeader, "")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")

	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	return body, resp.StatusCode, nil
}

func FetchBoardSectionPins(ctx context.Context, client *http.Client, cookiesHeader, headersJSON, userAgent, sourceURL, sectionID string) ([]byte, int, error) {
	options := map[string]any{
		"currentFilter":        -1,
		"field_set_key":        "react_grid_pin",
		"page_size":            25,
		"redux_normalize_feed": true,
		"section_id":           sectionID,
	}

	raw, _ := json.Marshal(map[string]any{"options": options, "context": map[string]any{}})
	params := url.Values{}
	params.Set("source_url", sourceURL)
	params.Set("data", string(raw))
	params.Set("_", strconv.FormatInt(nowFunc().UnixMilli(), 10))

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, HomefeedBaseURL+defaultBoardPinsPath+"?"+params.Encode(), nil)
	applyCapturedHeaders(req, headersJSON)
	applyDefaultHeaders(req, sourceURL, userAgent, cookiesHeader, defaultBoardPinsHandler)

	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	return body, resp.StatusCode, nil
}

func FetchBoardFeedPins(ctx context.Context, client *http.Client, cookiesHeader, headersJSON, userAgent, sourceURL, boardID string) ([]byte, int, error) {
	options := map[string]any{
		"board_id":             boardID,
		"board_url":            sourceURL,
		"currentFilter":        -1,
		"field_set_key":        "react_grid_pin",
		"filter_section_pins":  true,
		"sort":                 "default",
		"layout":               "featured_board",
		"page_size":            25,
		"redux_normalize_feed": true,
	}

	raw, _ := json.Marshal(map[string]any{"options": options, "context": map[string]any{}})
	params := url.Values{}
	params.Set("source_url", sourceURL)
	params.Set("data", string(raw))
	params.Set("_", strconv.FormatInt(nowFunc().UnixMilli(), 10))

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, HomefeedBaseURL+"/resource/BoardFeedResource/get/?"+params.Encode(), nil)
	applyCapturedHeaders(req, headersJSON)
	applyDefaultHeaders(req, sourceURL, userAgent, cookiesHeader, defaultBoardPageHandler)

	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	return body, resp.StatusCode, nil
}
