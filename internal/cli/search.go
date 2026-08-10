package cli

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/gongahkia/kaypoh/internal/app"
	"github.com/gongahkia/kaypoh/internal/domain"
)

func newSearchCommand(runtime *runtime) *cobra.Command {
	var date, fromDate, toDate, after, before, duration, maxPrice, maxPerPerson, near, membership, rank, origin, destination, travelMode string
	var sources, venueIDs, participantSpecs []string
	var radius, participants int
	var indoor, sheltered, explain bool
	command := &cobra.Command{
		Use:   "search <sport...>",
		Short: "Search fresh normalized availability without triggering a refresh",
		RunE: func(command *cobra.Command, args []string) error {
			service, err := runtime.openService(command.Context())
			if err != nil {
				return err
			}
			defer service.Close()
			criteria, err := buildSearchQuery(command, args, searchFlags{date: date, fromDate: fromDate, toDate: toDate, after: after, before: before, duration: duration, maxPrice: maxPrice, maxPerPerson: maxPerPerson, near: near, membership: membership, rank: rank, origin: origin, destination: destination, travelMode: travelMode, sources: sources, venueIDs: venueIDs, participantSpecs: participantSpecs, radius: radius, participants: participants, indoor: indoor, sheltered: sheltered}, service)
			if err != nil {
				return err
			}
			results, err := service.Search(command.Context(), criteria)
			if err != nil {
				return err
			}
			if runtime.json {
				return writeJSON(command.OutOrStdout(), results)
			}
			return writeSearchResults(command.OutOrStdout(), results, explain)
		},
	}
	command.Flags().StringVar(&date, "date", "", "local date YYYY-MM-DD (equivalent to --from and --to)")
	command.Flags().StringVar(&fromDate, "from", "", "first local date YYYY-MM-DD")
	command.Flags().StringVar(&toDate, "to", "", "last local date YYYY-MM-DD")
	command.Flags().StringVar(&after, "after", "", "earliest local start time HH:MM")
	command.Flags().StringVar(&before, "before", "", "latest local end time HH:MM")
	command.Flags().StringVar(&duration, "duration", "1h", "minimum contiguous duration, for example 90m")
	command.Flags().StringVar(&maxPrice, "max-price", "", "maximum total court price in SGD")
	command.Flags().StringVar(&maxPerPerson, "max-per-person", "", "maximum price per person in SGD")
	command.Flags().StringSliceVar(&sources, "source", nil, "require a source ID (repeatable)")
	command.Flags().StringSliceVar(&venueIDs, "venue", nil, "require a venue ID (repeatable)")
	command.Flags().BoolVar(&indoor, "indoor", false, "require indoor or explicitly non-indoor venues")
	command.Flags().BoolVar(&sheltered, "sheltered", false, "require sheltered or explicitly non-sheltered venues")
	command.Flags().StringVar(&membership, "membership", "", "membership requirement: required or not-required")
	command.Flags().StringVar(&near, "near", "", "latitude,longitude center for a radius filter")
	command.Flags().IntVar(&radius, "radius-m", 0, "radius in metres; requires --near")
	command.Flags().IntVar(&participants, "participants", 0, "number of players splitting the court price")
	command.Flags().StringVar(&rank, "rank", "balanced", "ranking preset: cheap, balanced, commute, or fair")
	command.Flags().StringVar(&origin, "origin", "", "single participant origin as coordinates or OneMap search text")
	command.Flags().StringVar(&destination, "destination", "", "single participant destination as coordinates or OneMap search text")
	command.Flags().StringVar(&travelMode, "travel-mode", "pt", "walk, cycle, drive, or pt")
	command.Flags().StringSliceVar(&participantSpecs, "participant", nil, "name|origin|destination-or--|mode; repeat for group commute ranking")
	command.Flags().BoolVar(&explain, "explain", false, "include ranking evidence and routing notes")
	return command
}

type searchFlags struct {
	date, fromDate, toDate, after, before, duration, maxPrice, maxPerPerson, near, membership, rank, origin, destination, travelMode string
	sources, venueIDs, participantSpecs                                                                                              []string
	radius, participants                                                                                                             int
	indoor, sheltered                                                                                                                bool
}

func buildSearchQuery(command *cobra.Command, sports []string, flags searchFlags, service *app.Service) (domain.Query, error) {
	criteria := domain.Query{Sports: sports, Sources: flags.sources, VenueIDs: flags.venueIDs, Participants: flags.participants, Ranking: domain.RankingPreset(flags.rank)}
	if flags.date != "" && (flags.fromDate != "" || flags.toDate != "") {
		return domain.Query{}, fmt.Errorf("--date cannot be combined with --from or --to")
	}
	if flags.date != "" {
		parsed, err := parseDate(flags.date)
		if err != nil {
			return domain.Query{}, err
		}
		criteria.StartDate, criteria.EndDate = &parsed, &parsed
	}
	if flags.fromDate != "" {
		parsed, err := parseDate(flags.fromDate)
		if err != nil {
			return domain.Query{}, err
		}
		criteria.StartDate = &parsed
	}
	if flags.toDate != "" {
		parsed, err := parseDate(flags.toDate)
		if err != nil {
			return domain.Query{}, err
		}
		criteria.EndDate = &parsed
	}
	if flags.after != "" {
		minutes, err := parseClock(flags.after, false)
		if err != nil {
			return domain.Query{}, fmt.Errorf("parse --after: %w", err)
		}
		criteria.EarliestStartMinute = &minutes
	}
	if flags.before != "" {
		minutes, err := parseClock(flags.before, true)
		if err != nil {
			return domain.Query{}, fmt.Errorf("parse --before: %w", err)
		}
		criteria.LatestEndMinute = &minutes
	}
	duration, err := time.ParseDuration(flags.duration)
	if err != nil || duration <= 0 {
		return domain.Query{}, fmt.Errorf("parse --duration: use a positive Go duration such as 90m")
	}
	criteria.MinimumDuration = duration
	if flags.maxPrice != "" {
		value, err := parseCents(flags.maxPrice)
		if err != nil {
			return domain.Query{}, fmt.Errorf("parse --max-price: %w", err)
		}
		criteria.MaximumPriceCents = &value
	}
	if flags.maxPerPerson != "" {
		value, err := parseCents(flags.maxPerPerson)
		if err != nil {
			return domain.Query{}, fmt.Errorf("parse --max-per-person: %w", err)
		}
		criteria.MaximumPricePerPersonCents = &value
	}
	if command.Flags().Changed("indoor") {
		criteria.Indoor = &flags.indoor
	}
	if command.Flags().Changed("sheltered") {
		criteria.Sheltered = &flags.sheltered
	}
	switch flags.membership {
	case "":
	case "required":
		value := true
		criteria.MembershipRequired = &value
	case "not-required":
		value := false
		criteria.MembershipRequired = &value
	default:
		return domain.Query{}, fmt.Errorf("--membership must be required or not-required")
	}
	if flags.near != "" || flags.radius != 0 {
		if flags.near == "" || flags.radius <= 0 {
			return domain.Query{}, fmt.Errorf("--near and positive --radius-m must be used together")
		}
		center, err := parseCoordinates(flags.near)
		if err != nil {
			return domain.Query{}, fmt.Errorf("parse --near: %w", err)
		}
		criteria.LocationRadius = &domain.LocationRadius{Center: center, RadiusMeters: flags.radius}
	}
	if len(flags.participantSpecs) > 0 && (flags.origin != "" || flags.destination != "") {
		return domain.Query{}, fmt.Errorf("use either --participant or --origin/--destination")
	}
	if flags.origin != "" {
		origin, err := resolveLocation(command.Context(), service, flags.origin)
		if err != nil {
			return domain.Query{}, fmt.Errorf("resolve --origin: %w", err)
		}
		criteria.Commute = &domain.CommuteProfile{Participants: []domain.Participant{{Name: "participant", Origin: origin, Mode: flags.travelMode}}}
		if flags.destination != "" {
			destination, err := resolveLocation(command.Context(), service, flags.destination)
			if err != nil {
				return domain.Query{}, fmt.Errorf("resolve --destination: %w", err)
			}
			criteria.Commute.Participants[0].Destination = &destination
		}
	}
	if len(flags.participantSpecs) > 0 {
		participants, err := parseParticipantSpecs(command.Context(), service, flags.participantSpecs)
		if err != nil {
			return domain.Query{}, err
		}
		criteria.Commute = &domain.CommuteProfile{Participants: participants}
	}
	return criteria, nil
}

func parseParticipantSpecs(ctx context.Context, service *app.Service, values []string) ([]domain.Participant, error) {
	participants := make([]domain.Participant, 0, len(values))
	for _, value := range values {
		parts := strings.Split(value, "|")
		if len(parts) != 4 {
			return nil, fmt.Errorf("--participant must be name|origin|destination-or--|mode")
		}
		origin, err := resolveLocation(ctx, service, parts[1])
		if err != nil {
			return nil, fmt.Errorf("resolve participant origin: %w", err)
		}
		participant := domain.Participant{Name: strings.TrimSpace(parts[0]), Origin: origin, Mode: strings.TrimSpace(parts[3])}
		if participant.Name == "" {
			participant.Name = "participant"
		}
		if strings.TrimSpace(parts[2]) != "-" {
			destination, err := resolveLocation(ctx, service, parts[2])
			if err != nil {
				return nil, fmt.Errorf("resolve participant destination: %w", err)
			}
			participant.Destination = &destination
		}
		participants = append(participants, participant)
	}
	return participants, nil
}

// resolveLocation only uses OneMap for an explicitly supplied text location.
// Direct coordinates keep search completely local and offline.
func resolveLocation(ctx context.Context, service *app.Service, value string) (domain.Coordinates, error) {
	if coordinates, err := parseCoordinates(value); err == nil {
		return coordinates, nil
	}
	places, err := service.Geocode(ctx, value)
	if err != nil {
		return domain.Coordinates{}, err
	}
	if len(places) == 0 {
		return domain.Coordinates{}, fmt.Errorf("no OneMap result")
	}
	return places[0].Coordinates, nil
}

func parseCoordinates(value string) (domain.Coordinates, error) {
	parts := strings.Split(strings.TrimSpace(value), ",")
	if len(parts) != 2 {
		return domain.Coordinates{}, fmt.Errorf("want latitude,longitude")
	}
	latitude, err := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
	if err != nil {
		return domain.Coordinates{}, err
	}
	longitude, err := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
	if err != nil {
		return domain.Coordinates{}, err
	}
	coordinates := domain.Coordinates{Latitude: latitude, Longitude: longitude}
	if !coordinates.Valid() || (coordinates.Latitude == 0 && coordinates.Longitude == 0) {
		return domain.Coordinates{}, fmt.Errorf("coordinates are outside the supported range")
	}
	return coordinates, nil
}

func parseDate(value string) (time.Time, error) {
	parsed, err := time.ParseInLocation("2006-01-02", strings.TrimSpace(value), singaporeLocation())
	if err != nil {
		return time.Time{}, fmt.Errorf("want YYYY-MM-DD: %w", err)
	}
	return parsed, nil
}

func parseClock(value string, allowEndOfDay bool) (int, error) {
	if allowEndOfDay && strings.TrimSpace(value) == "24:00" {
		return 24 * 60, nil
	}
	parsed, err := time.Parse("15:04", strings.TrimSpace(value))
	if err != nil {
		return 0, fmt.Errorf("want HH:MM: %w", err)
	}
	return parsed.Hour()*60 + parsed.Minute(), nil
}

func parseCents(value string) (int64, error) {
	value = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(value), "S$"), "SGD"))
	if value == "" || strings.HasPrefix(value, "-") {
		return 0, fmt.Errorf("amount must be a non-negative SGD decimal")
	}
	whole, fraction, hasFraction := strings.Cut(value, ".")
	if whole == "" {
		whole = "0"
	}
	wholeValue, err := strconv.ParseInt(whole, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid whole dollars")
	}
	if !hasFraction {
		return wholeValue * 100, nil
	}
	if len(fraction) > 2 || fraction == "" {
		return 0, fmt.Errorf("use at most two decimal places")
	}
	if len(fraction) == 1 {
		fraction += "0"
	}
	fractionValue, err := strconv.ParseInt(fraction, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid cents")
	}
	return wholeValue*100 + fractionValue, nil
}

func writeSearchResults(writer io.Writer, results []domain.SearchResult, explain bool) error {
	table := tabwriter.NewWriter(writer, 0, 4, 2, ' ', 0)
	fmt.Fprintln(table, "SCORE\tSTART\tEND\tSPORT\tVENUE\tPRICE\tTRAVEL\tSOURCE")
	for _, result := range results {
		fmt.Fprintf(table, "%.1f\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n", result.Breakdown.FinalScore, result.Slot.Start.In(singaporeLocation()).Format("2006-01-02 15:04"), result.Slot.End.In(singaporeLocation()).Format("15:04"), result.Slot.SportID, result.Venue.Name, formatCents(result.Breakdown.CourtPriceCents), formatDuration(result.Breakdown.TotalTravelSeconds), result.Slot.SourceID)
	}
	if err := table.Flush(); err != nil {
		return err
	}
	if explain {
		for _, result := range results {
			fmt.Fprintf(writer, "\n%s (%s)\n", result.Venue.Name, result.Slot.ID)
			for _, reason := range result.Breakdown.Reasons {
				fmt.Fprintf(writer, "  - %s\n", reason)
			}
		}
	}
	return nil
}

func formatCents(value *int64) string {
	if value == nil {
		return "—"
	}
	return fmt.Sprintf("S$%d.%02d", *value/100, *value%100)
}

func formatDuration(seconds int64) string {
	if seconds == 0 {
		return "—"
	}
	return fmt.Sprintf("%dm", seconds/60)
}

func singaporeLocation() *time.Location {
	location, err := time.LoadLocation(domain.SingaporeTimeZone)
	if err != nil {
		return time.FixedZone("SGT", 8*60*60)
	}
	return location
}
