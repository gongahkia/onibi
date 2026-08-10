package geo

import (
	"context"
	"errors"
	"math"
	"time"

	"github.com/gongahkia/courtsg/internal/domain"
)

var ErrCredentialsRequired = errors.New("routing credentials are not configured")

type TravelMode string

const (
	ModeWalk            TravelMode = "walk"
	ModeCycle           TravelMode = "cycle"
	ModeDrive           TravelMode = "drive"
	ModePublicTransport TravelMode = "pt"
)

func (mode TravelMode) Valid() bool {
	return mode == ModeWalk || mode == ModeCycle || mode == ModeDrive || mode == ModePublicTransport
}

type Place struct {
	Name        string             `json:"name"`
	Address     string             `json:"address"`
	PostalCode  string             `json:"postal_code,omitempty"`
	Coordinates domain.Coordinates `json:"coordinates"`
	Provider    string             `json:"provider"`
}

type Route struct {
	Origin         domain.Coordinates `json:"origin"`
	Destination    domain.Coordinates `json:"destination"`
	Mode           TravelMode         `json:"mode"`
	Duration       time.Duration      `json:"duration"`
	DistanceMeters int                `json:"distance_meters"`
	Provider       string             `json:"provider"`
	Fallback       bool               `json:"fallback"`
}

type Geocoder interface {
	Search(context.Context, string) ([]Place, error)
}

type Router interface {
	Route(context.Context, domain.Coordinates, domain.Coordinates, TravelMode) (Route, error)
}

// HaversineMeters calculates a deterministic straight-line distance.
func HaversineMeters(origin, destination domain.Coordinates) int {
	const earthRadiusMeters = 6_371_000.0
	lat1 := origin.Latitude * math.Pi / 180
	lat2 := destination.Latitude * math.Pi / 180
	deltaLat := (destination.Latitude - origin.Latitude) * math.Pi / 180
	deltaLon := (destination.Longitude - origin.Longitude) * math.Pi / 180
	a := math.Sin(deltaLat/2)*math.Sin(deltaLat/2) + math.Cos(lat1)*math.Cos(lat2)*math.Sin(deltaLon/2)*math.Sin(deltaLon/2)
	return int(math.Round(earthRadiusMeters * 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))))
}

func FallbackRoute(origin, destination domain.Coordinates, mode TravelMode) Route {
	distance := HaversineMeters(origin, destination)
	metersPerSecond := 1.2
	switch mode {
	case ModeCycle:
		metersPerSecond = 4.2
	case ModeDrive, ModePublicTransport:
		metersPerSecond = 8.3
	}
	return Route{
		Origin: origin, Destination: destination, Mode: mode, DistanceMeters: distance,
		Duration: time.Duration(float64(distance)/metersPerSecond) * time.Second,
		Provider: "haversine", Fallback: true,
	}
}
