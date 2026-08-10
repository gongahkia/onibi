package domain

import (
	"fmt"
	"sort"
	"strings"
)

// Sport is the canonical, stable representation used in every interface.
type Sport struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Aliases []string `json:"aliases,omitempty"`
}

var sports = []Sport{
	{ID: "badminton", Name: "Badminton", Aliases: []string{"badminton court", "shuttle"}},
	{ID: "basketball", Name: "Basketball", Aliases: []string{"basketball court", "bball"}},
	{ID: "football", Name: "Football", Aliases: []string{"soccer", "futsal", "football pitch"}},
	{ID: "padel", Name: "Padel", Aliases: []string{"paddle tennis"}},
	{ID: "pickleball", Name: "Pickleball", Aliases: []string{"pickle ball", "pickleball court"}},
	{ID: "squash", Name: "Squash", Aliases: []string{"squash court"}},
	{ID: "table-tennis", Name: "Table Tennis", Aliases: []string{"ping pong", "table tennis table", "tt"}},
	{ID: "tennis", Name: "Tennis", Aliases: []string{"tennis court"}},
	{ID: "volleyball", Name: "Volleyball", Aliases: []string{"volleyball court", "beach volleyball"}},
}

// Sports returns a copy sorted by canonical ID for stable output.
func Sports() []Sport {
	result := append([]Sport(nil), sports...)
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result
}

// CanonicalSport normalizes an ID, display name, or declared alias.
func CanonicalSport(value string) (Sport, error) {
	needle := normalizeSport(value)
	for _, sport := range sports {
		if needle == normalizeSport(sport.ID) || needle == normalizeSport(sport.Name) {
			return sport, nil
		}
		for _, alias := range sport.Aliases {
			if needle == normalizeSport(alias) {
				return sport, nil
			}
		}
	}
	return Sport{}, fmt.Errorf("unknown sport %q", value)
}

func normalizeSport(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, "_", "-")
	value = strings.Join(strings.Fields(value), " ")
	return strings.TrimSuffix(value, " court")
}
