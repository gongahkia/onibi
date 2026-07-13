package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/gongahkia/onibi/internal/config"
	"github.com/gongahkia/onibi/internal/store"
)

func profileCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "profile",
		Short: "Manage up profiles",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				return fmt.Errorf("unknown profile action %q", args[0])
			}
			return cmd.Help()
		},
	}
	add := &cobra.Command{
		Use:   "add <name>",
		Short: "Add an up profile",
		Args:  cobra.ExactArgs(1),
		RunE:  runProfileAdd,
	}
	add.Flags().String("transport", "", "pairing transport for this profile")
	add.Flags().String("agent", "", "agent executable for this profile")
	add.Flags().String("cwd", "", "working directory for this profile")
	add.Flags().Bool("use", false, "mark profile as last used")
	list := &cobra.Command{
		Use:   "list",
		Short: "List up profiles",
		Args:  cobra.ExactArgs(0),
		RunE:  runProfileList,
	}
	remove := &cobra.Command{
		Use:     "remove <name>",
		Aliases: []string{"rm", "delete"},
		Short:   "Remove an up profile",
		Args:    cobra.ExactArgs(1),
		RunE:    runProfileRemove,
	}
	use := &cobra.Command{
		Use:   "use <name>",
		Short: "Mark an up profile as last used",
		Args:  cobra.ExactArgs(1),
		RunE:  runProfileUse,
	}
	cmd.AddCommand(add, list, remove, use)
	return cmd
}

func runProfileAdd(cmd *cobra.Command, args []string) error {
	profile, err := profileFromFlags(cmd, args[0])
	if err != nil {
		return err
	}
	use, _ := cmd.Flags().GetBool("use")
	if use {
		profile.LastUsedAt = time.Now().UTC()
	}
	db, err := openDefaultDB()
	if err != nil {
		return err
	}
	defer db.Close()
	if err := db.ProfileUpsert(nonNilContext(cmd.Context()), profile); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Profile %s added\n", profile.Name)
	return nil
}

func runProfileList(cmd *cobra.Command, _ []string) error {
	db, err := openDefaultDB()
	if err != nil {
		return err
	}
	defer db.Close()
	profiles, err := db.ProfileList(nonNilContext(cmd.Context()))
	if err != nil {
		return err
	}
	if len(profiles) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "No profiles.")
		return nil
	}
	lastUsed := ""
	if !profiles[0].LastUsedAt.IsZero() {
		lastUsed = profiles[0].Name
	}
	for _, profile := range profiles {
		marker := " "
		if profile.Name == lastUsed {
			marker = "*"
		}
		fmt.Fprintf(cmd.OutOrStdout(), "%s %s\t%s\n", marker, profile.Name, profileSummary(profile))
	}
	return nil
}

func runProfileRemove(cmd *cobra.Command, args []string) error {
	db, err := openDefaultDB()
	if err != nil {
		return err
	}
	defer db.Close()
	removed, err := db.ProfileRemove(nonNilContext(cmd.Context()), args[0])
	if err != nil {
		return err
	}
	if !removed {
		return fmt.Errorf("profile %q not found", args[0])
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Profile %s removed\n", strings.TrimSpace(args[0]))
	return nil
}

func runProfileUse(cmd *cobra.Command, args []string) error {
	db, err := openDefaultDB()
	if err != nil {
		return err
	}
	defer db.Close()
	if err := db.ProfileTouch(nonNilContext(cmd.Context()), args[0], time.Now().UTC()); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Profile default: %s\n", strings.TrimSpace(args[0]))
	return nil
}

func profileFromFlags(cmd *cobra.Command, name string) (store.Profile, error) {
	transport, _ := cmd.Flags().GetString("transport")
	agent, _ := cmd.Flags().GetString("agent")
	cwd, _ := cmd.Flags().GetString("cwd")
	transport = strings.TrimSpace(transport)
	if transport != "" {
		cfg := config.Default()
		if err := config.Set(&cfg, "transport.mode", transport); err != nil {
			return store.Profile{}, err
		}
		transport = cfg.Transport.Mode
	}
	cwd = strings.TrimSpace(cwd)
	if cwd != "" {
		abs, err := filepath.Abs(cwd)
		if err != nil {
			return store.Profile{}, err
		}
		info, err := os.Stat(abs)
		if err != nil {
			return store.Profile{}, err
		}
		if !info.IsDir() {
			return store.Profile{}, fmt.Errorf("--cwd is not a directory: %s", abs)
		}
		cwd = abs
	}
	profile := store.Profile{
		Name:      strings.TrimSpace(name),
		Transport: transport,
		Agent:     strings.TrimSpace(agent),
		CWD:       cwd,
	}
	if profile.Transport == "" && profile.Agent == "" && profile.CWD == "" {
		return store.Profile{}, fmt.Errorf("profile %q has no flags", profile.Name)
	}
	return profile, nil
}

func profileSummary(profile store.Profile) string {
	var parts []string
	if profile.Transport != "" {
		parts = append(parts, "transport="+profile.Transport)
	}
	if profile.Agent != "" {
		parts = append(parts, "agent="+profile.Agent)
	}
	if profile.CWD != "" {
		parts = append(parts, "cwd="+profile.CWD)
	}
	return strings.Join(parts, " ")
}
