package geo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gongahkia/kaypoh/internal/domain"
	"github.com/gongahkia/kaypoh/internal/source"
)

const oneMapSourceID = "onemap"

type OneMapCredentials struct {
	Email       string
	Password    string
	AccessToken string
}

type OneMap struct {
	client      *source.HTTPClient
	credentials OneMapCredentials

	mu          sync.Mutex
	accessToken string
	expiresAt   time.Time
}

func NewOneMap(client *source.HTTPClient, credentials OneMapCredentials) (*OneMap, error) {
	if client == nil {
		return nil, errors.New("HTTP client is required")
	}
	if credentials.AccessToken == "" && (credentials.Email == "" || credentials.Password == "") {
		return nil, ErrCredentialsRequired
	}
	return &OneMap{client: client, credentials: credentials, accessToken: credentials.AccessToken}, nil
}

func (oneMap *OneMap) Search(ctx context.Context, query string) ([]Place, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, errors.New("geocode query cannot be empty")
	}
	token, err := oneMap.token(ctx)
	if err != nil {
		return nil, err
	}
	parameters := url.Values{"searchVal": {query}, "returnGeom": {"Y"}, "getAddrDetails": {"Y"}, "pageNum": {"1"}}
	response, err := oneMap.client.Fetch(ctx, source.HTTPRequest{
		SourceID: oneMapSourceID,
		URL:      "https://www.onemap.gov.sg/api/common/elastic/search?" + parameters.Encode(),
		Headers:  http.Header{"Authorization": []string{token}},
		CacheTTL: 30 * 24 * time.Hour,
	})
	if err != nil {
		return nil, err
	}
	return parseSearchResponse(response.Body)
}

func (oneMap *OneMap) Route(ctx context.Context, origin, destination domain.Coordinates, mode TravelMode) (Route, error) {
	if !origin.Valid() || !destination.Valid() {
		return Route{}, errors.New("route coordinates are invalid")
	}
	if !mode.Valid() {
		return Route{}, fmt.Errorf("invalid travel mode %q", mode)
	}
	token, err := oneMap.token(ctx)
	if err != nil {
		return Route{}, err
	}
	parameters := url.Values{
		"start":     {coordinateString(origin)},
		"end":       {coordinateString(destination)},
		"routeType": {string(mode)},
	}
	response, err := oneMap.client.Fetch(ctx, source.HTTPRequest{
		SourceID: oneMapSourceID,
		URL:      "https://www.onemap.gov.sg/api/public/routingsvc/route?" + parameters.Encode(),
		Headers:  http.Header{"Authorization": []string{token}},
		CacheTTL: 6 * time.Hour,
	})
	if err != nil {
		return Route{}, err
	}
	return parseRouteResponse(response.Body, origin, destination, mode)
}

func (oneMap *OneMap) token(ctx context.Context) (string, error) {
	oneMap.mu.Lock()
	defer oneMap.mu.Unlock()
	if oneMap.accessToken != "" && (oneMap.expiresAt.IsZero() || time.Now().Add(5*time.Minute).Before(oneMap.expiresAt)) {
		return oneMap.accessToken, nil
	}
	payload, err := json.Marshal(map[string]string{"email": oneMap.credentials.Email, "password": oneMap.credentials.Password})
	if err != nil {
		return "", fmt.Errorf("encode OneMap authentication request: %w", err)
	}
	response, err := oneMap.client.Fetch(ctx, source.HTTPRequest{
		SourceID: oneMapSourceID,
		Method:   http.MethodPost,
		URL:      "https://www.onemap.gov.sg/api/auth/post/getToken",
		Headers:  http.Header{"Content-Type": []string{"application/json"}},
		Body:     payload,
	})
	if err != nil {
		return "", err
	}
	var tokenResponse struct {
		AccessToken     string `json:"access_token"`
		ExpiryTimestamp string `json:"expiry_timestamp"`
	}
	if err := json.Unmarshal(response.Body, &tokenResponse); err != nil {
		return "", fmt.Errorf("decode OneMap authentication response: %w", err)
	}
	if tokenResponse.AccessToken == "" {
		return "", errors.New("OneMap authentication response omitted access_token")
	}
	if tokenResponse.ExpiryTimestamp != "" {
		seconds, err := strconv.ParseInt(tokenResponse.ExpiryTimestamp, 10, 64)
		if err != nil {
			return "", fmt.Errorf("parse OneMap token expiry: %w", err)
		}
		oneMap.expiresAt = time.Unix(seconds, 0)
	}
	oneMap.accessToken = tokenResponse.AccessToken
	return oneMap.accessToken, nil
}

func parseSearchResponse(body []byte) ([]Place, error) {
	var response struct {
		Error   string `json:"error"`
		Results []struct {
			SearchValue string `json:"SEARCHVAL"`
			Address     string `json:"ADDRESS"`
			Postal      string `json:"POSTAL"`
			Latitude    string `json:"LATITUDE"`
			Longitude   string `json:"LONGITUDE"`
		} `json:"results"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("decode OneMap search response: %w", err)
	}
	if response.Error != "" {
		return nil, fmt.Errorf("OneMap search: %s", response.Error)
	}
	places := make([]Place, 0, len(response.Results))
	for _, result := range response.Results {
		latitude, err := strconv.ParseFloat(result.Latitude, 64)
		if err != nil {
			return nil, fmt.Errorf("parse OneMap latitude %q: %w", result.Latitude, err)
		}
		longitude, err := strconv.ParseFloat(result.Longitude, 64)
		if err != nil {
			return nil, fmt.Errorf("parse OneMap longitude %q: %w", result.Longitude, err)
		}
		coordinates := domain.Coordinates{Latitude: latitude, Longitude: longitude}
		if !coordinates.Valid() {
			return nil, fmt.Errorf("OneMap result contains invalid coordinates")
		}
		places = append(places, Place{Name: result.SearchValue, Address: result.Address, PostalCode: result.Postal, Coordinates: coordinates, Provider: "onemap"})
	}
	return places, nil
}

func parseRouteResponse(body []byte, origin, destination domain.Coordinates, mode TravelMode) (Route, error) {
	var response struct {
		Status        int    `json:"status"`
		StatusMessage string `json:"status_message"`
		RouteSummary  struct {
			TotalTime     int `json:"total_time"`
			TotalDistance int `json:"total_distance"`
		} `json:"route_summary"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return Route{}, fmt.Errorf("decode OneMap routing response: %w", err)
	}
	if response.Status != 0 {
		return Route{}, fmt.Errorf("OneMap route status %d: %s", response.Status, response.StatusMessage)
	}
	if response.RouteSummary.TotalTime < 0 || response.RouteSummary.TotalDistance < 0 {
		return Route{}, errors.New("OneMap route response contains negative values")
	}
	return Route{
		Origin: origin, Destination: destination, Mode: mode,
		Duration:       time.Duration(response.RouteSummary.TotalTime) * time.Minute,
		DistanceMeters: response.RouteSummary.TotalDistance, Provider: "onemap",
	}, nil
}

func coordinateString(coordinates domain.Coordinates) string {
	return strconv.FormatFloat(coordinates.Latitude, 'f', 6, 64) + "," + strconv.FormatFloat(coordinates.Longitude, 'f', 6, 64)
}
