package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/gongahkia/onibi/internal/config"
)

func configCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Show and edit config.yaml",
		RunE:  runConfig,
	}
	cmd.Flags().Bool("show", false, "print effective config")
	cmd.Flags().Bool("list", false, "list supported config keys")
	cmd.Flags().Bool("get", false, "print one config value")
	cmd.Flags().Bool("set", false, "set one config value")
	cmd.Flags().Bool("init", false, "create config.yaml with defaults")
	cmd.Flags().Bool("validate", false, "validate config.yaml")
	cmd.Flags().Bool("path", false, "print config and state paths")
	cmd.Flags().Bool("json", false, "print JSON with --show")
	cmd.Flags().Bool("force", false, "overwrite config.yaml with --init")
	cmd.AddCommand(
		configShowCmd(),
		configListCmd(),
		configGetCmd(),
		configSetCmd(),
		configInitCmd(),
		configValidateCmd(),
		configPathCmd(),
	)
	return cmd
}

func runConfig(cmd *cobra.Command, args []string) error {
	action, err := selectedActionFlag(cmd, "show", "list", "get", "set", "init", "validate", "path")
	if err != nil {
		return err
	}
	switch action {
	case "show":
		if err := cobra.ExactArgs(0)(cmd, args); err != nil {
			return err
		}
		return runConfigShow(cmd)
	case "list":
		if err := cobra.ExactArgs(0)(cmd, args); err != nil {
			return err
		}
		return runConfigList(cmd)
	case "get":
		if err := cobra.ExactArgs(1)(cmd, args); err != nil {
			return err
		}
		return runConfigGet(cmd, args)
	case "set":
		if err := cobra.ExactArgs(2)(cmd, args); err != nil {
			return err
		}
		return runConfigSet(cmd, args)
	case "init":
		if err := cobra.ExactArgs(0)(cmd, args); err != nil {
			return err
		}
		return runConfigInit(cmd)
	case "validate":
		if err := cobra.ExactArgs(0)(cmd, args); err != nil {
			return err
		}
		return runConfigValidate(cmd)
	case "path":
		if err := cobra.ExactArgs(0)(cmd, args); err != nil {
			return err
		}
		return runConfigPath(cmd)
	default:
		return showActionHelp(cmd, args, "show", "list", "get", "set", "init", "validate", "path")
	}
}

func configShowCmd() *cobra.Command {
	cmd := &cobra.Command{
		Hidden: true,
		Use:   "show",
		Short: "Print effective config",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runConfigShow(cmd)
		},
	}
	cmd.Flags().Bool("json", false, "print JSON")
	return cmd
}

func configListCmd() *cobra.Command {
	return &cobra.Command{
		Hidden: true,
		Use:   "list",
		Short: "List supported config keys",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runConfigList(cmd)
		},
	}
}

func configGetCmd() *cobra.Command {
	return &cobra.Command{
		Hidden: true,
		Use:   "get <key>",
		Short: "Print one config value",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runConfigGet(cmd, args)
		},
	}
}

func configSetCmd() *cobra.Command {
	return &cobra.Command{
		Hidden: true,
		Use:   "set <key> <value>",
		Short: "Set one config value",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runConfigSet(cmd, args)
		},
	}
}

func configInitCmd() *cobra.Command {
	cmd := &cobra.Command{
		Hidden: true,
		Use:   "init",
		Short: "Create config.yaml with defaults",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runConfigInit(cmd)
		},
	}
	cmd.Flags().Bool("force", false, "overwrite existing config.yaml")
	return cmd
}

func configValidateCmd() *cobra.Command {
	return &cobra.Command{
		Hidden: true,
		Use:   "validate",
		Short: "Validate config.yaml",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runConfigValidate(cmd)
		},
	}
}

func configPathCmd() *cobra.Command {
	return &cobra.Command{
		Hidden: true,
		Use:   "path",
		Short: "Print config and state paths",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runConfigPath(cmd)
		},
	}
}

func runConfigShow(cmd *cobra.Command) error {
	jsonOut, _ := cmd.Flags().GetBool("json")
	cfg, meta, err := loadConfig()
	if err != nil {
		return err
	}
	if jsonOut {
		return json.NewEncoder(cmd.OutOrStdout()).Encode(map[string]any{
			"path":   meta.Path,
			"loaded": meta.Exists,
			"config": cfg,
		})
	}
	style := styleFor(cmd)
	fmt.Fprintf(cmd.OutOrStdout(), "%s: %s\n", style.bold("path"), meta.Path)
	if meta.Exists {
		fmt.Fprintf(cmd.OutOrStdout(), "%s: file\n", style.bold("source"))
	} else {
		fmt.Fprintf(cmd.OutOrStdout(), "%s: defaults (run `onibi config --init` to create file)\n", style.bold("source"))
	}
	b, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	_, err = cmd.OutOrStdout().Write(b)
	return err
}

func runConfigList(cmd *cobra.Command) error {
	cfg, meta, err := loadConfig()
	if err != nil {
		return err
	}
	style := styleFor(cmd)
	table := [][]string{tableHeader(style, "KEY", "CURRENT", "DEFAULT", "SET", "DESCRIPTION")}
	for _, row := range config.Keys(cfg, meta) {
		set := "no"
		if row.Explicit {
			set = "yes"
		}
		table = append(table, []string{row.Key, row.Current, row.Default, styleConfigSet(style, row.Explicit, set), row.Description})
	}
	return renderTable(cmd.OutOrStdout(), table)
}

func runConfigGet(cmd *cobra.Command, args []string) error {
	cfg, _, err := loadConfig()
	if err != nil {
		return err
	}
	v, err := config.Get(cfg, args[0])
	if err != nil {
		return err
	}
	fmt.Fprintln(cmd.OutOrStdout(), v)
	return nil
}

func runConfigSet(cmd *cobra.Command, args []string) error {
	cfg, meta, err := loadConfig()
	if err != nil {
		return err
	}
	if err := config.Set(&cfg, args[0], args[1]); err != nil {
		return err
	}
	if err := config.Save(meta.Path, cfg); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "%s Set %s=%s in %s\n", styleFor(cmd).green("[OK]"), args[0], args[1], meta.Path)
	return nil
}

func runConfigInit(cmd *cobra.Command) error {
	force, _ := cmd.Flags().GetBool("force")
	paths, err := config.DefaultPaths()
	if err != nil {
		return err
	}
	if err := paths.EnsureDirs(); err != nil {
		return err
	}
	if _, err := os.Stat(paths.Config); err == nil && !force {
		return fmt.Errorf("%s exists; pass --force to overwrite", paths.Config)
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := config.Save(paths.Config, config.Default()); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "%s Created %s\n", styleFor(cmd).green("[OK]"), paths.Config)
	return nil
}

func runConfigValidate(cmd *cobra.Command) error {
	_, meta, err := loadConfig()
	if err != nil {
		return err
	}
	if meta.Exists {
		fmt.Fprintf(cmd.OutOrStdout(), "%s %s\n", styleFor(cmd).green("[OK]"), meta.Path)
	} else {
		fmt.Fprintf(cmd.OutOrStdout(), "%s %s (defaults; file not present)\n", styleFor(cmd).green("[OK]"), meta.Path)
	}
	return nil
}

func runConfigPath(cmd *cobra.Command) error {
	paths, err := config.DefaultPaths()
	if err != nil {
		return err
	}
	style := styleFor(cmd)
	return renderTable(cmd.OutOrStdout(), [][]string{
		{style.bold("config"), paths.Config},
		{style.bold("state"), paths.StateDir},
		{style.bold("db"), paths.DBFile},
		{style.bold("socket"), paths.Socket},
		{style.bold("env_fallback"), paths.EnvFile},
		{style.bold("logs"), paths.LogDir},
	})
}

func loadConfig() (config.Config, config.LoadMeta, error) {
	paths, err := config.DefaultPaths()
	if err != nil {
		return config.Config{}, config.LoadMeta{}, err
	}
	return config.Load(paths)
}

func styleConfigSet(style cliStyle, explicit bool, text string) string {
	if explicit {
		return style.green(text)
	}
	return style.dim(text)
}
