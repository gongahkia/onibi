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
	}
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

func configShowCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "show",
		Short: "Print effective config",
		RunE: func(cmd *cobra.Command, _ []string) error {
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
			fmt.Fprintf(cmd.OutOrStdout(), "path: %s\n", meta.Path)
			if meta.Exists {
				fmt.Fprintln(cmd.OutOrStdout(), "source: file")
			} else {
				fmt.Fprintln(cmd.OutOrStdout(), "source: defaults (run `onibi config init` to create file)")
			}
			b, err := yaml.Marshal(cfg)
			if err != nil {
				return err
			}
			_, err = cmd.OutOrStdout().Write(b)
			return err
		},
	}
	cmd.Flags().Bool("json", false, "print JSON")
	return cmd
}

func configListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List supported config keys",
		RunE: func(cmd *cobra.Command, _ []string) error {
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
		},
	}
}

func configGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <key>",
		Short: "Print one config value",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
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
		},
	}
}

func configSetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "set <key> <value>",
		Short: "Set one config value",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
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
			fmt.Fprintf(cmd.OutOrStdout(), "Set %s=%s in %s\n", args[0], args[1], meta.Path)
			return nil
		},
	}
}

func configInitCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Create config.yaml with defaults",
		RunE: func(cmd *cobra.Command, _ []string) error {
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
			fmt.Fprintf(cmd.OutOrStdout(), "Created %s\n", paths.Config)
			return nil
		},
	}
	cmd.Flags().Bool("force", false, "overwrite existing config.yaml")
	return cmd
}

func configValidateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "validate",
		Short: "Validate config.yaml",
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, meta, err := loadConfig()
			if err != nil {
				return err
			}
			if meta.Exists {
				fmt.Fprintf(cmd.OutOrStdout(), "OK %s\n", meta.Path)
			} else {
				fmt.Fprintf(cmd.OutOrStdout(), "OK %s (defaults; file not present)\n", meta.Path)
			}
			return nil
		},
	}
}

func configPathCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "path",
		Short: "Print config and state paths",
		RunE: func(cmd *cobra.Command, _ []string) error {
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
		},
	}
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
