package services

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	neturl "net/url"
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

type boardSectionPinsPayload struct {
	Options struct {
		CurrentFilter     int    `json:"currentFilter"`
		FieldSetKey       string `json:"field_set_key"`
		IsOwnProfilePins  bool   `json:"is_own_profile_pins"`
		PageSize          int    `json:"page_size"`
		ReduxNormalizeFee bool   `json:"redux_normalize_feed"`
		SectionID         string `json:"section_id"`
		OrbacSubjectID    string `json:"orbac_subject_id"`
	} `json:"options"`
	Context map[string]any `json:"context"`
}

type boardParentPayload struct {
	Options map[string]any `json:"options"`
	Context map[string]any `json:"context"`
}

func ParseBoardRef(raw string) (*BoardRef, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, ErrMissingBoardURL
	}

	if strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") {
		parsed, err := neturl.Parse(raw)
		if err != nil {
			return nil, ErrInvalidBoardURL
		}
		raw = parsed.Path
	}

	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, ErrInvalidBoardURL
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

func FetchBoard(ctx context.Context, client *http.Client, cookiesHeader string, headersJSON string, userAgent string, rawURL string) (*BoardResponse, int, error) {
	if client == nil {
		return nil, 0, errors.New("http client is nil")
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
		Sections: make([]BoardSectionResult, 0, len(sections)),
	}

	if len(sections) == 0 && metadata != nil {
		// No sections found, fetch from board feed
		body, feedStatus, feedErr := FetchBoardFeedPins(
			ctx,
			client,
			cookiesHeader,
			headersJSON,
			userAgent,
			ref.SourceURL,
			metadata.ID,
		)
		if feedErr == nil && feedStatus >= 200 && feedStatus < 300 {
			pins, pinErr := parsers.ExtractSearchPinsFromJSON(string(body))
			if pinErr == nil {
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
		sectionURL := buildBoardSectionSourceURL(ref.SourceURL, section.Slug)
		sectionResult := BoardSectionResult{
			ID:    section.ID,
			Slug:  section.Slug,
			Title: section.Title,
			URL:   HomefeedBaseURL + sectionURL,
		}

		body, sectionStatus, sectionErr := FetchBoardSectionPins(
			ctx,
			client,
			cookiesHeader,
			headersJSON,
			userAgent,
			sectionURL,
			section.ID,
		)
		if sectionErr != nil {
			sectionResult.Error = sectionErr.Error()
			result.Sections = append(result.Sections, sectionResult)
			continue
		}
		if sectionStatus < 200 || sectionStatus >= 300 {
			sectionResult.Error = "upstream returned status " + http.StatusText(sectionStatus)
			result.Sections = append(result.Sections, sectionResult)
			continue
		}

		pins, pinErr := parsers.ExtractSearchPinsFromJSON(string(body))
		if pinErr != nil {
			sectionResult.Error = pinErr.Error()
			result.Sections = append(result.Sections, sectionResult)
			continue
		}

		sectionResult.Pins = pins
		sectionResult.Count = len(pins)
		result.Sections = append(result.Sections, sectionResult)
	}

	result.SectionCount = len(result.Sections)
	return result, status, nil
}

func discoverBoardSections(ctx context.Context, client *http.Client, cookiesHeader string, headersJSON string, userAgent string, sourceURL string, pageBody []byte) ([]parsers.BoardSection, error) {
	if sections, err := parsers.ExtractBoardSectionsFromHTML(string(pageBody)); err == nil {
		return sections, nil
	}

	if publicHTML, err := FetchHTML(HomefeedBaseURL + sourceURL); err == nil {
		if sections, err := parsers.ExtractBoardSectionsFromHTML(publicHTML); err == nil {
			return sections, nil
		}
	}

	parentBody, _, err := FetchBoardParentResource(ctx, client, cookiesHeader, headersJSON, userAgent, sourceURL)
	if err != nil {
		return nil, err
	}
	return parsers.ExtractBoardSectionsFromJSON(string(parentBody))
}

func buildBoardSectionSourceURL(boardSourceURL string, sectionSlug string) string {
	return boardSourceURL + strings.Trim(sectionSlug, "/") + "/"
}

func FetchBoardParentResource(ctx context.Context, client *http.Client, cookiesHeader string, headersJSON string, userAgent string, sourceURL string) ([]byte, int, error) {
	payload := boardParentPayload{
		Options: map[string]any{
			"data": map[string]any{
				"appVersion":     "",
				"auxData":        map[string]any{"stage": "prod"},
				"browser":        1,
				"clientUUID":     "",
				"event_type":     7137,
				"time":           nowFunc().UnixNano(),
				"unauth_id":      "",
				"view_type":      5,
				"view_parameter": 3070,
			},
		},
		Context: map[string]any{},
	}

	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, 0, err
	}

	form := url.Values{}
	form.Set("source_url", sourceURL)
	form.Set("data", string(raw))

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, HomefeedBaseURL+defaultBoardParentPath, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, 0, err
	}

	applyCapturedHeaders(req, headersJSON)
	applyDefaultHeaders(req, sourceURL, userAgent, cookiesHeader)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", defaultOrigin)
	req.Header.Set("X-Pinterest-PWS-Handler", defaultBoardPageHandler)
	req.Header.Set("X-Pinterest-AppState", defaultAppState)

	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, err
	}

	return body, resp.StatusCode, nil
}

func FetchBoardPage(ctx context.Context, client *http.Client, cookiesHeader string, headersJSON string, userAgent string, sourceURL string) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, HomefeedBaseURL+sourceURL, nil)
	if err != nil {
		return nil, 0, err
	}

	applyCapturedHeaders(req, headersJSON)
	if userAgent != "" {
		req.Header.Set("User-Agent", userAgent)
	} else {
		req.Header.Set("User-Agent", defaultUserAgent)
	}
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", defaultAcceptLang)
	req.Header.Set("Referer", defaultReferer)
	if cookiesHeader != "" {
		req.Header.Set("Cookie", cookiesHeader)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, err
	}

	return body, resp.StatusCode, nil
}

func FetchBoardSectionPins(ctx context.Context, client *http.Client, cookiesHeader string, headersJSON string, userAgent string, sourceURL string, sectionID string) ([]byte, int, error) {
	if sectionID == "" {
		return nil, 0, errors.New("missing section id")
	}

	payload := boardSectionPinsPayload{
		Context: map[string]any{},
	}
	payload.Options.CurrentFilter = -1
	payload.Options.FieldSetKey = "react_grid_pin"
	payload.Options.IsOwnProfilePins = false
	payload.Options.PageSize = 25
	payload.Options.ReduxNormalizeFee = true
	payload.Options.SectionID = sectionID
	payload.Options.OrbacSubjectID = ""

	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, 0, err
	}

	requestURL := HomefeedBaseURL + defaultBoardPinsPath + "?source_url=" + neturl.QueryEscape(sourceURL) + "&data=" + neturl.QueryEscape(string(raw)) + "&_=" + strconv.FormatInt(nowFunc().UnixMilli(), 10)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return nil, 0, err
	}

	applyCapturedHeaders(req, headersJSON)
	applyDefaultHeaders(req, sourceURL, userAgent, cookiesHeader)
	req.Header.Set("X-Pinterest-PWS-Handler", defaultBoardPinsHandler)
	req.Header.Set("X-Pinterest-AppState", defaultAppState)

	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, err
	}

	return body, resp.StatusCode, nil
}
func FetchBoardFeedPins(ctx context.Context, client *http.Client, cookiesHeader string, headersJSON string, userAgent string, sourceURL string, boardID string) ([]byte, int, error) {
	payload := map[string]any{
		"options": map[string]any{
			"board_id":             boardID,
			"board_url":            sourceURL,
			"currentFilter":        -1,
			"field_set_key":        "react_grid_pin",
			"filter_section_pins":  true,
			"sort":                 "default",
			"layout":               "featured_board",
			"page_size":            25,
			"redux_normalize_feed": true,
		},
		"context": map[string]any{},
	}

	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, 0, err
	}

	params := url.Values{}
	params.Set("source_url", sourceURL)
	params.Set("data", string(raw))
	params.Set("_", strconv.FormatInt(nowFunc().UnixMilli(), 10))

	requestURL := HomefeedBaseURL + "/resource/BoardFeedResource/get/?" + params.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return nil, 0, err
	}

	applyCapturedHeaders(req, headersJSON)
	applyDefaultHeaders(req, sourceURL, userAgent, cookiesHeader)

	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, err
	}

	return body, resp.StatusCode, nil
}
