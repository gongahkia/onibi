package cli

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/gongahkia/kaypoh/internal/domain"
)

func newAvailabilityCommand(runtime *runtime) *cobra.Command {
	command := &cobra.Command{Use: "availability", Aliases: []string{"slot"}, Short: "Manage locally supplied availability records"}
	var start, end, price, bookingURL, id string
	var membershipRequired bool
	add := &cobra.Command{
		Use:   "add <venue-id>",
		Short: "Add a local availability interval without contacting an upstream source",
		Args:  cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			startAt, err := parseTimestamp(start)
			if err != nil {
				return fmt.Errorf("parse --start: %w", err)
			}
			endAt, err := parseTimestamp(end)
			if err != nil {
				return fmt.Errorf("parse --end: %w", err)
			}
			var priceCents *int64
			if strings.TrimSpace(price) != "" {
				value, err := parseCents(price)
				if err != nil {
					return fmt.Errorf("parse --price: %w", err)
				}
				priceCents = &value
			}
			slot := domain.AvailabilitySlot{ID: id, VenueID: args[0], Start: startAt, End: endAt, PriceCents: priceCents, BookingURL: bookingURL}
			if command.Flags().Changed("membership-required") {
				slot.MembershipRequired = &membershipRequired
			}
			service, err := runtime.openService(command.Context())
			if err != nil {
				return err
			}
			defer service.Close()
			saved, err := service.ImportManualAvailability(command.Context(), []domain.AvailabilitySlot{slot})
			if err != nil {
				return err
			}
			slot = saved[0]
			if runtime.json {
				return writeJSON(command.OutOrStdout(), slot)
			}
			_, err = fmt.Fprintf(command.OutOrStdout(), "saved %s at %s from %s to %s\n", slot.ID, slot.VenueID, slot.Start.In(singaporeLocation()).Format("2006-01-02 15:04 MST"), slot.End.In(singaporeLocation()).Format("15:04 MST"))
			return err
		},
	}
	add.Flags().StringVar(&start, "start", "", "slot start as RFC 3339 or 2006-01-02T15:04 in Asia/Singapore")
	add.Flags().StringVar(&end, "end", "", "slot end as RFC 3339 or 2006-01-02T15:04 in Asia/Singapore")
	add.Flags().StringVar(&price, "price", "", "optional total court price in SGD, for example 12.50")
	add.Flags().StringVar(&bookingURL, "booking-url", "", "optional authorized booking link")
	add.Flags().StringVar(&id, "id", "", "optional stable local slot ID")
	add.Flags().BoolVar(&membershipRequired, "membership-required", false, "whether this interval requires membership")
	_ = add.MarkFlagRequired("start")
	_ = add.MarkFlagRequired("end")
	command.AddCommand(add)
	return command
}

func parseTimestamp(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	if parsed, err := time.Parse(time.RFC3339, value); err == nil {
		return parsed, nil
	}
	parsed, err := time.ParseInLocation("2006-01-02T15:04", value, singaporeLocation())
	if err != nil {
		return time.Time{}, fmt.Errorf("want RFC 3339 or 2006-01-02T15:04: %w", err)
	}
	return parsed, nil
}
