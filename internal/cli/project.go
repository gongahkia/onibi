package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/gongahkia/onibi/internal/config"
	"github.com/gongahkia/onibi/internal/store"
)

const cliProjectAliasPrefix = "project_alias:"

func runProject(cmd *cobra.Command, args []string) error {
	action, err := selectedActionFlag(cmd, "list", "add", "forget")
	if err != nil {
		return err
	}
	switch action {
	case "list":
		if err := cobra.ExactArgs(0)(cmd, args); err != nil {
			return err
		}
		return runProjectList(cmd, args)
	case "add":
		if err := cobra.MinimumNArgs(1)(cmd, args); err != nil {
			return err
		}
		return runProjectAdd(cmd, args)
	case "forget":
		if err := cobra.ExactArgs(1)(cmd, args); err != nil {
			return err
		}
		return runProjectForget(cmd, args)
	default:
		return showActionHelp(cmd, args, "list", "add", "forget")
	}
}

func runProjectList(cmd *cobra.Command, _ []string) error {
	db, closeDB, err := openProjectDB()
	if err != nil {
		return err
	}
	defer closeDB()
	ctx := nonNilContext(cmd.Context())
	keys, err := db.KVKeysWithPrefix(ctx, cliProjectAliasPrefix)
	if err != nil {
		return err
	}
	if len(keys) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "No project aliases.")
		return nil
	}
	for _, key := range keys {
		alias := strings.TrimPrefix(key, cliProjectAliasPrefix)
		path, ok, _ := db.KVGetString(ctx, key)
		if ok {
			fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\n", alias, path)
		}
	}
	return nil
}

func runProjectAdd(cmd *cobra.Command, args []string) error {
	alias, path, err := parseCLIProjectAdd(args)
	if err != nil {
		return err
	}
	db, closeDB, err := openProjectDB()
	if err != nil {
		return err
	}
	defer closeDB()
	if err := db.KVSetString(nonNilContext(cmd.Context()), cliProjectAliasKey(alias), path); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Project %s saved: %s\n", alias, path)
	return nil
}

func runProjectForget(cmd *cobra.Command, args []string) error {
	alias := cliSanitizeProjectAlias(args[0])
	if alias == "" {
		return errors.New("project alias required")
	}
	db, closeDB, err := openProjectDB()
	if err != nil {
		return err
	}
	defer closeDB()
	if err := db.KVDel(nonNilContext(cmd.Context()), cliProjectAliasKey(alias)); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Project forgotten: %s\n", alias)
	return nil
}

func parseCLIProjectAdd(args []string) (string, string, error) {
	if len(args) == 1 && args[0] == "here" {
		cwd, err := os.Getwd()
		if err != nil {
			return "", "", err
		}
		alias := cliSanitizeProjectAlias(filepath.Base(cwd))
		if alias == "" {
			return "", "", errors.New("current directory does not produce a project alias")
		}
		return alias, cwd, nil
	}
	if len(args) < 2 {
		return "", "", errors.New("usage: onibi project --add here | onibi project --add <alias> <path>")
	}
	alias := cliSanitizeProjectAlias(args[0])
	if alias == "" {
		return "", "", errors.New("project alias must contain letters, numbers, dot, underscore, or dash")
	}
	path, err := cliNormalizeProjectPath(strings.Join(args[1:], " "))
	if err != nil {
		return "", "", err
	}
	return alias, path, nil
}

func openProjectDB() (*store.DB, func(), error) {
	paths, err := config.DefaultPaths()
	if err != nil {
		return nil, nil, err
	}
	if err := paths.EnsureDirs(); err != nil {
		return nil, nil, err
	}
	db, err := openDefaultDB()
	if err != nil {
		return nil, nil, err
	}
	return db, func() { _ = db.Close() }, nil
}

func cliProjectAliasKey(alias string) string {
	return cliProjectAliasPrefix + alias
}

func cliSanitizeProjectAlias(alias string) string {
	alias = strings.ToLower(strings.TrimSpace(alias))
	var b strings.Builder
	for _, r := range alias {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '.' || r == '_' || r == '-' {
			b.WriteRune(r)
		}
	}
	runes := []rune(b.String())
	if len(runes) > 32 {
		return string(runes[:32])
	}
	return string(runes)
}

func cliNormalizeProjectPath(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", errors.New("project path required")
	}
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		path = filepath.Join(home, strings.TrimPrefix(path, "~/"))
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	st, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("project path unavailable: %w", err)
	}
	if !st.IsDir() {
		return "", errors.New("project path is not a directory")
	}
	return abs, nil
}

func nonNilContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}
