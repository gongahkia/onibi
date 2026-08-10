package cli

import (
	"fmt"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/gongahkia/kaypoh/internal/domain"
)

func newWatchCommand(runtime *runtime) *cobra.Command {
	command := &cobra.Command{Use: "watch", Aliases: []string{"watches"}, Short: "Create and evaluate persistent availability watches"}
	command.AddCommand(newWatchAddCommand(runtime))
	var enabledOnly bool
	list := &cobra.Command{
		Use:   "list",
		Short: "List stored watches",
		RunE: func(command *cobra.Command, args []string) error {
			service, err := runtime.openService(command.Context())
			if err != nil {
				return err
			}
			defer service.Close()
			watches, err := service.Watches(command.Context(), enabledOnly)
			if err != nil {
				return err
			}
			if runtime.json {
				return writeJSON(command.OutOrStdout(), watches)
			}
			return writeWatches(command, watches)
		},
	}
	list.Flags().BoolVar(&enabledOnly, "enabled", false, "show only enabled watches")
	command.AddCommand(list)
	command.AddCommand(&cobra.Command{
		Use:   "show <watch-id>",
		Short: "Show a watch and its evaluation state",
		Args:  cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			service, err := runtime.openService(command.Context())
			if err != nil {
				return err
			}
			defer service.Close()
			watch, err := service.Watch(command.Context(), args[0])
			if err != nil {
				return err
			}
			state, err := service.WatchState(command.Context(), args[0])
			if err != nil {
				return err
			}
			if runtime.json {
				return writeJSON(command.OutOrStdout(), map[string]any{"watch": watch, "state": state})
			}
			_, err = fmt.Fprintf(command.OutOrStdout(), "id: %s\nname: %s\nenabled: %t\none shot: %t\nlast evaluated: %v\nlast match: %v\nlast error: %s\n", watch.ID, watch.Name, watch.Enabled, watch.OneShot, state.LastEvaluatedAt, state.LastMatchAt, state.LastError)
			return err
		},
	})
	for _, action := range []struct {
		name    string
		enabled bool
	}{{name: "enable", enabled: true}, {name: "disable", enabled: false}} {
		action := action
		command.AddCommand(&cobra.Command{
			Use:   action.name + " <watch-id>",
			Short: action.name + " a watch",
			Args:  cobra.ExactArgs(1),
			RunE: func(command *cobra.Command, args []string) error {
				service, err := runtime.openService(command.Context())
				if err != nil {
					return err
				}
				defer service.Close()
				if err := service.SetWatchEnabled(command.Context(), args[0], action.enabled); err != nil {
					return err
				}
				watch, err := service.Watch(command.Context(), args[0])
				if err != nil {
					return err
				}
				if runtime.json {
					return writeJSON(command.OutOrStdout(), watch)
				}
				_, err = fmt.Fprintf(command.OutOrStdout(), "%s %s\n", action.name, args[0])
				return err
			},
		})
	}
	command.AddCommand(&cobra.Command{
		Use:   "delete <watch-id>",
		Short: "Delete one watch and its stored events",
		Args:  cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			service, err := runtime.openService(command.Context())
			if err != nil {
				return err
			}
			defer service.Close()
			if err := service.DeleteWatch(command.Context(), args[0]); err != nil {
				return err
			}
			if runtime.json {
				return writeJSON(command.OutOrStdout(), map[string]string{"id": args[0], "status": "deleted"})
			}
			_, err = fmt.Fprintf(command.OutOrStdout(), "deleted %s\n", args[0])
			return err
		},
	})
	command.AddCommand(&cobra.Command{
		Use:   "evaluate",
		Short: "Evaluate each enabled watch against local availability",
		RunE: func(command *cobra.Command, args []string) error {
			service, err := runtime.openService(command.Context())
			if err != nil {
				return err
			}
			defer service.Close()
			results, err := service.EvaluateWatches(command.Context())
			if err != nil {
				return err
			}
			if runtime.json {
				return writeJSON(command.OutOrStdout(), results)
			}
			table := tabwriter.NewWriter(command.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintln(table, "WATCH\tSTATE\tMATCHES\tNEW EVENTS\tDETAIL")
			for _, result := range results {
				fmt.Fprintf(table, "%s\t%s\t%d\t%d\t%s\n", result.WatchID, result.State, result.Matches, result.EventsCreated, result.Detail)
			}
			return table.Flush()
		},
	})
	var eventWatchID string
	var eventLimit int
	events := &cobra.Command{
		Use:   "events",
		Short: "List idempotent watch events",
		RunE: func(command *cobra.Command, args []string) error {
			service, err := runtime.openService(command.Context())
			if err != nil {
				return err
			}
			defer service.Close()
			events, err := service.Events(command.Context(), eventWatchID, eventLimit)
			if err != nil {
				return err
			}
			if runtime.json {
				return writeJSON(command.OutOrStdout(), events)
			}
			for _, event := range events {
				fmt.Fprintf(command.OutOrStdout(), "%s\t%s\t%s\n", event.ID, event.WatchID, event.CreatedAt.In(singaporeLocation()).Format("2006-01-02 15:04:05"))
			}
			return nil
		},
	}
	events.Flags().StringVar(&eventWatchID, "watch", "", "filter by watch ID")
	events.Flags().IntVar(&eventLimit, "limit", 100, "maximum events (1-1000)")
	command.AddCommand(events)
	var deliveryEventID string
	var deliveryLimit int
	deliveries := &cobra.Command{
		Use:   "deliveries",
		Short: "List webhook and Telegram delivery outcomes",
		RunE: func(command *cobra.Command, args []string) error {
			service, err := runtime.openService(command.Context())
			if err != nil {
				return err
			}
			defer service.Close()
			results, err := service.Deliveries(command.Context(), deliveryEventID, deliveryLimit)
			if err != nil {
				return err
			}
			if runtime.json {
				return writeJSON(command.OutOrStdout(), results)
			}
			table := tabwriter.NewWriter(command.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintln(table, "EVENT\tTARGET\tSTATUS\tATTEMPTS\tERROR")
			for _, result := range results {
				fmt.Fprintf(table, "%s\t%s\t%s\t%d\t%s\n", result.EventID, result.TargetID, result.Status, result.Attempts, result.Error)
			}
			return table.Flush()
		},
	}
	deliveries.Flags().StringVar(&deliveryEventID, "event", "", "filter by event ID")
	deliveries.Flags().IntVar(&deliveryLimit, "limit", 100, "maximum deliveries (1-1000)")
	command.AddCommand(deliveries)
	var retryLimit int
	retry := &cobra.Command{
		Use:   "retry-deliveries",
		Short: "Retry pending or failed deliveries up to five total attempts",
		RunE: func(command *cobra.Command, args []string) error {
			service, err := runtime.openService(command.Context())
			if err != nil {
				return err
			}
			defer service.Close()
			results, err := service.RetryDeliveries(command.Context(), 5, retryLimit)
			if err != nil {
				return err
			}
			if runtime.json {
				return writeJSON(command.OutOrStdout(), results)
			}
			_, err = fmt.Fprintf(command.OutOrStdout(), "retried %d delivery record(s)\n", len(results))
			return err
		},
	}
	retry.Flags().IntVar(&retryLimit, "limit", 100, "maximum deliveries to retry (1-1000)")
	command.AddCommand(retry)
	return command
}

func newWatchAddCommand(runtime *runtime) *cobra.Command {
	var duration, maxPrice, date, expiresAt string
	var sources, venues, webhookNames []string
	var telegramChats []int64
	var oneShot bool
	command := &cobra.Command{
		Use:   "add <name> <sport...>",
		Short: "Create a persistent watch for local availability matches",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(command *cobra.Command, args []string) error {
			service, err := runtime.openService(command.Context())
			if err != nil {
				return err
			}
			defer service.Close()
			criteria, err := buildSearchQuery(command, args[1:], searchFlags{date: date, duration: duration, maxPrice: maxPrice, sources: sources, venueIDs: venues, rank: "balanced"}, service)
			if err != nil {
				return err
			}
			var expires *time.Time
			if strings.TrimSpace(expiresAt) != "" {
				parsed, err := parseTimestamp(expiresAt)
				if err != nil {
					return fmt.Errorf("parse --expires-at: %w", err)
				}
				expires = &parsed
			}
			targets := make([]domain.NotificationTarget, 0, len(telegramChats)+len(webhookNames))
			for _, chatID := range telegramChats {
				targets = append(targets, domain.NotificationTarget{Kind: domain.NotificationTelegram, ChatID: chatID})
			}
			for _, name := range webhookNames {
				targets = append(targets, domain.NotificationTarget{Kind: domain.NotificationWebhook, WebhookName: name})
			}
			watch, err := service.CreateWatch(command.Context(), domain.Watch{Name: args[0], Query: criteria, Targets: targets, Enabled: true, OneShot: oneShot, ExpiresAt: expires})
			if err != nil {
				return err
			}
			if runtime.json {
				return writeJSON(command.OutOrStdout(), watch)
			}
			_, err = fmt.Fprintf(command.OutOrStdout(), "created %s\n", watch.ID)
			return err
		},
	}
	command.Flags().StringVar(&duration, "duration", "1h", "minimum contiguous duration")
	command.Flags().StringVar(&maxPrice, "max-price", "", "maximum total court price in SGD")
	command.Flags().StringVar(&date, "date", "", "local date YYYY-MM-DD")
	command.Flags().StringSliceVar(&sources, "source", nil, "require a source ID")
	command.Flags().StringSliceVar(&venues, "venue", nil, "require a venue ID")
	command.Flags().BoolVar(&oneShot, "one-shot", false, "disable after the first new matching event")
	command.Flags().StringVar(&expiresAt, "expires-at", "", "expiry as RFC 3339 or 2006-01-02T15:04 in Asia/Singapore")
	command.Flags().Int64SliceVar(&telegramChats, "telegram-chat", nil, "Telegram chat ID target; repeatable")
	command.Flags().StringSliceVar(&webhookNames, "webhook", nil, "configured webhook name target; repeatable")
	return command
}

func writeWatches(command *cobra.Command, watches []domain.Watch) error {
	table := tabwriter.NewWriter(command.OutOrStdout(), 0, 4, 2, ' ', 0)
	fmt.Fprintln(table, "ID\tENABLED\tONE SHOT\tNAME\tSPORTS\tEXPIRES")
	for _, watch := range watches {
		expires := ""
		if watch.ExpiresAt != nil {
			expires = watch.ExpiresAt.In(singaporeLocation()).Format("2006-01-02 15:04")
		}
		fmt.Fprintf(table, "%s\t%t\t%t\t%s\t%s\t%s\n", watch.ID, watch.Enabled, watch.OneShot, watch.Name, strings.Join(watch.Query.Sports, ","), expires)
	}
	return table.Flush()
}
