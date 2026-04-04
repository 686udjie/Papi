package internal

import (
	"errors"

	"papi/parsers"
)

var (
	ErrMissingInput = errors.New("missing id or url")
	ErrInvalidPinID = errors.New("invalid pin id")
	ErrPinNotFound  = errors.New("pin not found")
)

var fetchHTML = FetchHTML

func FetchPinterest(id string, rawURL string) (*parsers.Response, error) {
	if id == "" && rawURL == "" {
		return nil, ErrMissingInput
	}

	if id == "" {
		id = ExtractPinID(rawURL)
	}

	if id == "" {
		return nil, ErrInvalidPinID
	}

	html, err := fetchHTML("https://www.pinterest.com/pin/" + id + "/")
	if err != nil {
		return nil, err
	}

	if jsonData, err := parsers.ExtractJSON(html); err == nil {
		if resp, err := parsers.ParsePinterestJSON(jsonData, id); err == nil {
			return resp, nil
		}
	}

	if resp, ok := parsers.ParsePinterestHTML(html, id); ok {
		return resp, nil
	}

	return nil, ErrPinNotFound
}
