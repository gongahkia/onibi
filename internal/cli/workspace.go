package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/gongahkia/onibi/internal/workspace"
)

func workspaceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:               "workspace",
		Short:             "Manage portable project workspace configuration",
		PersistentPreRunE: requireExperimentalWorkspace,
	}
	init := &cobra.Command{
		Use:   "init [directory]",
		Short: "Create .onibi/workspace.toml without host bindings",
		Args:  cobra.MaximumNArgs(1),
		RunE:  runWorkspaceInit,
	}
	init.Flags().String("name", "", "portable workspace name")
	init.Flags().String("agent", "", "optional default agent preference")
	validate := &cobra.Command{
		Use:   "validate [path]",
		Short: "Validate a portable .onibi/workspace.toml file",
		Args:  cobra.MaximumNArgs(1),
		RunE:  runWorkspaceValidate,
	}
	cmd.AddCommand(init, validate)
	return cmd
}

func runWorkspaceInit(cmd *cobra.Command, args []string) error {
	root := "."
	if len(args) == 1 {
		root = args[0]
	}
	path, err := workspace.ProjectPath(root)
	if err != nil {
		return err
	}
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("workspace file already exists: %s", path)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	name, _ := cmd.Flags().GetString("name")
	agent, _ := cmd.Flags().GetString("agent")
	if err := workspace.SaveProjectConfig(path, workspace.ProjectConfig{Name: strings.TrimSpace(name), DefaultAgent: strings.TrimSpace(agent)}); err != nil {
		return err
	}
	fmt.Fprintln(cmd.OutOrStdout(), "Created portable workspace:", path)
	return nil
}

func runWorkspaceValidate(cmd *cobra.Command, args []string) error {
	path := ""
	if len(args) == 0 {
		var err error
		path, err = workspace.ProjectPath(".")
		if err != nil {
			return err
		}
	} else {
		path = strings.TrimSpace(args[0])
		if info, err := os.Stat(path); err == nil && info.IsDir() {
			path, err = workspace.ProjectPath(path)
			if err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
	}
	path, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	if filepath.Base(path) != "workspace.toml" || filepath.Base(filepath.Dir(path)) != ".onibi" {
		return errors.New("workspace path must be .onibi/workspace.toml")
	}
	cfg, err := workspace.LoadProjectConfig(path)
	if err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Valid portable workspace %s: %s\n", cfg.Name, path)
	return nil
}
