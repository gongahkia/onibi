package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/gongahkia/courtsg/internal/app"
	"github.com/gongahkia/courtsg/internal/config"
	"github.com/gongahkia/courtsg/internal/store"
)

type runtime struct {
	configPath string
	database   string
	json       bool
}

func New(version string) *cobra.Command {
	runtime := &runtime{}
	root := &cobra.Command{
		Use:           "courtsg",
		Short:         "Singapore sports-facility discovery and availability monitoring",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(command *cobra.Command, args []string) error {
			return errors.New("the interactive TUI is not available until the next milestone; use `courtsg --help`")
		},
	}
	root.PersistentFlags().StringVar(&runtime.configPath, "config", "", "config file path")
	root.PersistentFlags().StringVar(&runtime.database, "db", "", "SQLite database path override")
	root.PersistentFlags().BoolVar(&runtime.json, "json", false, "emit stable JSON to stdout")
	root.AddCommand(newVersionCommand(version))
	root.AddCommand(newConfigCommand(runtime))
	root.AddCommand(newSourcesCommand(runtime))
	root.AddCommand(newRefreshCommand(runtime))
	root.AddCommand(newVenueCommand(runtime))
	root.AddCommand(newDoctorCommand(runtime))
	return root
}

func newRefreshCommand(runtime *runtime) *cobra.Command {
	return &cobra.Command{
		Use:   "refresh [source-id...]",
		Short: "Refresh each selected permitted source once",
		RunE: func(command *cobra.Command, args []string) error {
			service, err := runtime.openService(command.Context())
			if err != nil {
				return err
			}
			defer service.Close()
			results, err := service.Refresh(command.Context(), args)
			if err != nil {
				return err
			}
			if runtime.json {
				return writeJSON(command.OutOrStdout(), results)
			}
			table := tabwriter.NewWriter(command.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintln(table, "SOURCE\tSTATE\tVENUES\tDETAIL")
			for _, result := range results {
				fmt.Fprintf(table, "%s\t%s\t%d\t%s\n", result.SourceID, result.State, result.VenuesUpdated, result.Detail)
			}
			return table.Flush()
		},
	}
}

func newVenueCommand(runtime *runtime) *cobra.Command {
	command := &cobra.Command{Use: "venue", Aliases: []string{"venues"}, Short: "Search normalized venues"}
	var search string
	var sports []string
	var limit int
	list := &cobra.Command{
		Use:   "list",
		Short: "List venues already discovered from permitted sources",
		RunE: func(command *cobra.Command, args []string) error {
			service, err := runtime.openService(command.Context())
			if err != nil {
				return err
			}
			defer service.Close()
			venues, err := service.Venues(command.Context(), store.VenueFilter{Search: search, Sports: sports, Limit: limit})
			if err != nil {
				return err
			}
			if runtime.json {
				return writeJSON(command.OutOrStdout(), venues)
			}
			table := tabwriter.NewWriter(command.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintln(table, "ID\tNAME\tADDRESS\tPOSTAL CODE")
			for _, venue := range venues {
				fmt.Fprintf(table, "%s\t%s\t%s\t%s\n", venue.ID, venue.Name, venue.Address, venue.PostalCode)
			}
			return table.Flush()
		},
	}
	list.Flags().StringVar(&search, "search", "", "case-insensitive venue name or address match")
	list.Flags().StringSliceVar(&sports, "sport", nil, "require each canonical sport ID")
	list.Flags().IntVar(&limit, "limit", 100, "maximum records (1-500)")
	command.AddCommand(list)
	command.AddCommand(&cobra.Command{
		Use:   "show <venue-id>",
		Short: "Show venue provenance and booking links",
		Args:  cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			service, err := runtime.openService(command.Context())
			if err != nil {
				return err
			}
			defer service.Close()
			venue, err := service.Venue(command.Context(), args[0])
			if err != nil {
				return err
			}
			if runtime.json {
				return writeJSON(command.OutOrStdout(), venue)
			}
			_, err = fmt.Fprintf(command.OutOrStdout(), "id: %s\nname: %s\naddress: %s\nbooking links: %v\nsource: %s\nfetched: %s\n", venue.ID, venue.Name, venue.Address, venue.BookingURLs, venue.Provenance.SourceID, venue.Provenance.FetchedAt.Format("2006-01-02 15:04:05 MST"))
			return err
		},
	})
	return command
}

func (runtime *runtime) openService(ctx context.Context) (*app.Service, error) {
	path := runtime.configPath
	if path == "" {
		var err error
		path, err = config.DefaultPath()
		if err != nil {
			return nil, err
		}
	}
	cfg, err := config.Load(path)
	if err != nil {
		return nil, err
	}
	if runtime.database != "" {
		cfg.DatabasePath = runtime.database
		cfg.DataDir = filepath.Dir(runtime.database)
	}
	return app.Open(ctx, cfg)
}

func newVersionCommand(version string) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the version",
		Run: func(command *cobra.Command, args []string) {
			fmt.Fprintln(command.OutOrStdout(), version)
		},
	}
}

func newConfigCommand(runtime *runtime) *cobra.Command {
	command := &cobra.Command{Use: "config", Short: "Inspect and initialize configuration"}
	command.AddCommand(&cobra.Command{
		Use:   "path",
		Short: "Print the default config path",
		RunE: func(command *cobra.Command, args []string) error {
			path, err := config.DefaultPath()
			if err != nil {
				return err
			}
			if runtime.json {
				return writeJSON(command.OutOrStdout(), map[string]string{"path": path})
			}
			_, err = fmt.Fprintln(command.OutOrStdout(), path)
			return err
		},
	})
	command.AddCommand(&cobra.Command{
		Use:   "init",
		Short: "Write a non-secret example configuration if none exists",
		RunE: func(command *cobra.Command, args []string) error {
			path := runtime.configPath
			if path == "" {
				var err error
				path, err = config.DefaultPath()
				if err != nil {
					return err
				}
			}
			if err := config.WriteExample(path); err != nil {
				return err
			}
			if runtime.json {
				return writeJSON(command.OutOrStdout(), map[string]string{"path": path, "status": "created"})
			}
			_, err := fmt.Fprintf(command.OutOrStdout(), "created %s\n", path)
			return err
		},
	})
	command.AddCommand(&cobra.Command{
		Use:   "validate",
		Short: "Validate configuration without printing secrets",
		RunE: func(command *cobra.Command, args []string) error {
			path := runtime.configPath
			if path == "" {
				var err error
				path, err = config.DefaultPath()
				if err != nil {
					return err
				}
			}
			if _, err := config.Load(path); err != nil {
				return err
			}
			if runtime.json {
				return writeJSON(command.OutOrStdout(), map[string]string{"path": path, "status": "valid"})
			}
			_, err := fmt.Fprintln(command.OutOrStdout(), "valid")
			return err
		},
	})
	return command
}

func newSourcesCommand(runtime *runtime) *cobra.Command {
	command := &cobra.Command{Use: "sources", Short: "Inspect source coverage and compliance state"}
	command.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List configured sources",
		RunE: func(command *cobra.Command, args []string) error {
			service, err := runtime.openService(command.Context())
			if err != nil {
				return err
			}
			defer service.Close()
			records, err := service.Sources(command.Context())
			if err != nil {
				return err
			}
			return writeSources(command.OutOrStdout(), records, runtime.json)
		},
	})
	command.AddCommand(&cobra.Command{
		Use:   "show <source-id>",
		Short: "Show one source's capability and policy state",
		Args:  cobra.ExactArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			service, err := runtime.openService(command.Context())
			if err != nil {
				return err
			}
			defer service.Close()
			record, err := service.Source(command.Context(), args[0])
			if err != nil {
				return err
			}
			if runtime.json {
				return writeJSON(command.OutOrStdout(), record)
			}
			return writeSource(command.OutOrStdout(), record)
		},
	})
	for _, action := range []struct {
		name    string
		enabled bool
	}{
		{name: "enable", enabled: true},
		{name: "disable", enabled: false},
	} {
		action := action
		command.AddCommand(&cobra.Command{
			Use:   action.name + " <source-id>",
			Short: action.name + " a policy-permitted source",
			Args:  cobra.ExactArgs(1),
			RunE: func(command *cobra.Command, args []string) error {
				service, err := runtime.openService(command.Context())
				if err != nil {
					return err
				}
				defer service.Close()
				record, err := service.SetSourceEnabled(command.Context(), args[0], action.enabled)
				if err != nil {
					return err
				}
				if runtime.json {
					return writeJSON(command.OutOrStdout(), record)
				}
				return writeSource(command.OutOrStdout(), record)
			},
		})
	}
	command.AddCommand(&cobra.Command{
		Use:   "doctor",
		Short: "Report source health without touching disabled sources",
		RunE: func(command *cobra.Command, args []string) error {
			service, err := runtime.openService(command.Context())
			if err != nil {
				return err
			}
			defer service.Close()
			records, err := service.SourceDoctor(command.Context())
			if err != nil {
				return err
			}
			return writeSources(command.OutOrStdout(), records, runtime.json)
		},
	})
	return command
}

func newDoctorCommand(runtime *runtime) *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Diagnose local storage, configuration, and source policy state",
		RunE: func(command *cobra.Command, args []string) error {
			service, err := runtime.openService(command.Context())
			if err != nil {
				return err
			}
			defer service.Close()
			report, err := service.Doctor(command.Context())
			if err != nil {
				return err
			}
			if runtime.json {
				return writeJSON(command.OutOrStdout(), report)
			}
			writer := tabwriter.NewWriter(command.OutOrStdout(), 0, 4, 2, ' ', 0)
			for _, check := range report.Checks {
				fmt.Fprintf(writer, "%s\t%s\t%s\n", check.Name, check.State, check.Detail)
			}
			return writer.Flush()
		},
	}
}

func writeSources(writer io.Writer, records []store.SourceRecord, asJSON bool) error {
	if asJSON {
		return writeJSON(writer, records)
	}
	table := tabwriter.NewWriter(writer, 0, 4, 2, ' ', 0)
	fmt.Fprintln(table, "ID\tENABLED\tPOLICY\tHEALTH\tNAME")
	for _, record := range records {
		fmt.Fprintf(table, "%s\t%t\t%s\t%s\t%s\n", record.Info.ID, record.Enabled, record.Info.Policy.Status, record.Health.State, record.Info.Name)
	}
	return table.Flush()
}

func writeSource(writer io.Writer, record store.SourceRecord) error {
	_, err := fmt.Fprintf(writer, "id: %s\nname: %s\noperator: %s\nenabled: %t\npolicy: %s\nhealth: %s\nwebsite: %s\nnotes: %s\n", record.Info.ID, record.Info.Name, record.Info.Operator, record.Enabled, record.Info.Policy.Status, record.Health.State, record.Info.Website, record.Info.Policy.Notes)
	return err
}

func writeJSON(writer io.Writer, value any) error {
	encoder := json.NewEncoder(writer)
	encoder.SetIndent("", "  ")
	encoder.SetEscapeHTML(false)
	return encoder.Encode(value)
}

func Execute(ctx context.Context, version string, args []string, stdout, stderr io.Writer) int {
	command := New(version)
	command.SetArgs(args)
	command.SetOut(stdout)
	command.SetErr(stderr)
	command.SetContext(ctx)
	if err := command.Execute(); err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	return 0
}

func DefaultExecute(version string) int {
	return Execute(context.Background(), version, os.Args[1:], os.Stdout, os.Stderr)
}
