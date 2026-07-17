package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/gongahkia/onibi/internal/config"
	"github.com/gongahkia/onibi/internal/store"
	onibiworkspace "github.com/gongahkia/onibi/internal/workspace"
)

func workspaceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "workspace",
		Short: "Manage workspaces",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				return fmt.Errorf("unknown workspace action %q", args[0])
			}
			return cmd.Help()
		},
	}
	add := &cobra.Command{
		Use:   "add <name> [path]",
		Short: "Add a private workspace binding",
		Args:  cobra.RangeArgs(1, 2),
		RunE:  runWorkspaceAdd,
	}
	add.Flags().String("ssh-key", "", "SSH key reference for this workspace")
	add.Flags().String("default-transport", "", "workspace transport override")
	add.Flags().Bool("use", false, "set as default workspace after adding")
	list := &cobra.Command{
		Use:   "list",
		Short: "List private workspace bindings",
		Args:  cobra.ExactArgs(0),
		RunE:  runWorkspaceList,
	}
	use := &cobra.Command{
		Use:   "use <name|path>",
		Short: "Set the default workspace",
		Args:  cobra.ExactArgs(1),
		RunE:  runWorkspaceUse,
	}
	remove := &cobra.Command{
		Use:     "remove <name>",
		Aliases: []string{"rm", "delete"},
		Short:   "Remove a private workspace binding",
		Args:    cobra.ExactArgs(1),
		RunE:    runWorkspaceRemove,
	}
	exportCmd := &cobra.Command{
		Use:   "export <name> <path>",
		Short: "Export a portable workspace bundle",
		Args:  cobra.ExactArgs(2),
		RunE:  runWorkspaceExport,
	}
	importCmd := &cobra.Command{
		Use:   "import <path>",
		Short: "Import a portable workspace bundle",
		Args:  cobra.ExactArgs(1),
		RunE:  runWorkspaceImport,
	}
	importCmd.Flags().String("ssh-key", "", "SSH key reference for this workspace")
	importCmd.Flags().Bool("use", false, "set as default workspace after importing")
	cmd.AddCommand(add, list, use, remove, exportCmd, importCmd)
	return cmd
}

func runWorkspaceAdd(cmd *cobra.Command, args []string) error {
	name := strings.TrimSpace(args[0])
	path := "."
	if len(args) == 2 {
		path = args[1]
	}
	root, err := cliNormalizeProjectPath(path)
	if err != nil {
		return err
	}
	sshKey, _ := cmd.Flags().GetString("ssh-key")
	defaultTransport, _ := cmd.Flags().GetString("default-transport")
	if strings.TrimSpace(defaultTransport) == "" {
		defaultTransport = projectDefaultTransport(root, name)
	}
	db, wsStore, closeDB, err := openWorkspaceDB()
	if err != nil {
		return err
	}
	defer closeDB()
	now := time.Now().UTC()
	if err := saveWorkspaceBinding(nonNilContext(cmd.Context()), wsStore, onibiworkspace.IndexEntry{
		Name:             name,
		Path:             root,
		LastSeen:         now,
		SSHKey:           strings.TrimSpace(sshKey),
		DefaultTransport: strings.TrimSpace(defaultTransport),
	}); err != nil {
		return err
	}
	use, _ := cmd.Flags().GetBool("use")
	if use {
		if err := onibiworkspace.SetDefaultName(nonNilContext(cmd.Context()), db, name); err != nil {
			return err
		}
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Workspace %s added: %s\n", name, root)
	return nil
}

func runWorkspaceList(cmd *cobra.Command, _ []string) error {
	db, wsStore, closeDB, err := openWorkspaceDB()
	if err != nil {
		return err
	}
	defer closeDB()
	ctx := nonNilContext(cmd.Context())
	entries, err := wsStore.List(ctx)
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "No workspaces.")
		return nil
	}
	defaultName, _, _ := onibiworkspace.DefaultName(ctx, db)
	for _, entry := range entries {
		marker := " "
		if entry.Name == defaultName {
			marker = "*"
		}
		transport := ""
		if indexEntry, ok := loadWorkspaceIndex(entry.Name); ok {
			transport = indexEntry.DefaultTransport
		}
		if transport == "" {
			fmt.Fprintf(cmd.OutOrStdout(), "%s %s\t%s\n", marker, entry.Name, entry.Path)
			continue
		}
		fmt.Fprintf(cmd.OutOrStdout(), "%s %s\t%s\t%s\n", marker, entry.Name, entry.Path, transport)
	}
	return nil
}

func runWorkspaceUse(cmd *cobra.Command, args []string) error {
	name := strings.TrimSpace(args[0])
	db, wsStore, closeDB, err := openWorkspaceDB()
	if err != nil {
		return err
	}
	defer closeDB()
	ctx := nonNilContext(cmd.Context())
	entry, ok, err := wsStore.Get(ctx, name)
	if err != nil {
		if pathErr := runWorkspaceUsePath(cmd, db, wsStore, args[0]); pathErr == nil {
			return nil
		}
		return err
	}
	if !ok {
		return runWorkspaceUsePath(cmd, db, wsStore, args[0])
	}
	entry.LastSeen = time.Now().UTC()
	if err := wsStore.Upsert(ctx, entry); err != nil {
		return err
	}
	if indexEntry, ok := loadWorkspaceIndex(name); ok {
		indexEntry.LastSeen = entry.LastSeen
		_ = saveWorkspaceIndex(indexEntry)
	}
	if err := onibiworkspace.SetDefaultName(ctx, db, name); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Workspace default: %s\n", name)
	return nil
}

func runWorkspaceUsePath(cmd *cobra.Command, db *store.DB, wsStore *onibiworkspace.DBStore, arg string) error {
	projectFile, err := projectFileFromArg(arg)
	if err != nil {
		return fmt.Errorf("workspace %q not found", arg)
	}
	cfg, err := onibiworkspace.LoadProjectConfig(projectFile.Path)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	if err := saveWorkspaceBinding(nonNilContext(cmd.Context()), wsStore, onibiworkspace.IndexEntry{
		Name:             cfg.Name,
		Path:             projectFile.Root,
		LastSeen:         now,
		DefaultTransport: cfg.Transports.Default,
	}); err != nil {
		return err
	}
	if err := onibiworkspace.SetDefaultName(nonNilContext(cmd.Context()), db, cfg.Name); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Workspace default: %s\n", cfg.Name)
	return nil
}

func runWorkspaceRemove(cmd *cobra.Command, args []string) error {
	name := strings.TrimSpace(args[0])
	db, wsStore, closeDB, err := openWorkspaceDB()
	if err != nil {
		return err
	}
	defer closeDB()
	ctx := nonNilContext(cmd.Context())
	removed, err := wsStore.Remove(ctx, name)
	if err != nil {
		return err
	}
	indexRemoved, err := removeWorkspaceIndex(name)
	if err != nil {
		return err
	}
	if !removed && !indexRemoved {
		return fmt.Errorf("workspace %q not found", name)
	}
	if err := onibiworkspace.ClearDefaultName(ctx, db, name); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Workspace removed: %s\n", name)
	return nil
}

func runWorkspaceExport(cmd *cobra.Command, args []string) error {
	name := strings.TrimSpace(args[0])
	dstRoot, err := filepath.Abs(strings.TrimSpace(args[1]))
	if err != nil {
		return err
	}
	_, wsStore, closeDB, err := openWorkspaceDB()
	if err != nil {
		return err
	}
	defer closeDB()
	entry, ok, err := wsStore.Get(nonNilContext(cmd.Context()), name)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("workspace %q not found", name)
	}
	if err := exportWorkspaceBundle(entry, dstRoot); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Workspace %s exported: %s\n", name, dstRoot)
	return nil
}

func runWorkspaceImport(cmd *cobra.Command, args []string) error {
	projectFile, err := projectFileFromArg(args[0])
	if err != nil {
		return err
	}
	cfg, err := onibiworkspace.LoadProjectConfig(projectFile.Path)
	if err != nil {
		return err
	}
	sshKey, _ := cmd.Flags().GetString("ssh-key")
	db, wsStore, closeDB, err := openWorkspaceDB()
	if err != nil {
		return err
	}
	defer closeDB()
	now := time.Now().UTC()
	if err := saveWorkspaceBinding(nonNilContext(cmd.Context()), wsStore, onibiworkspace.IndexEntry{
		Name:             cfg.Name,
		Path:             projectFile.Root,
		LastSeen:         now,
		SSHKey:           strings.TrimSpace(sshKey),
		DefaultTransport: cfg.Transports.Default,
	}); err != nil {
		return err
	}
	use, _ := cmd.Flags().GetBool("use")
	if use {
		if err := onibiworkspace.SetDefaultName(nonNilContext(cmd.Context()), db, cfg.Name); err != nil {
			return err
		}
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Workspace %s imported: %s\n", cfg.Name, projectFile.Root)
	return nil
}

func openWorkspaceDB() (*store.DB, *onibiworkspace.DBStore, func(), error) {
	paths, err := config.DefaultPaths()
	if err != nil {
		return nil, nil, nil, err
	}
	if err := paths.EnsureDirs(); err != nil {
		return nil, nil, nil, err
	}
	db, err := openDefaultDB()
	if err != nil {
		return nil, nil, nil, err
	}
	wsStore, err := onibiworkspace.NewDBStore(db)
	if err != nil {
		_ = db.Close()
		return nil, nil, nil, err
	}
	return db, wsStore, func() { _ = db.Close() }, nil
}

func saveWorkspaceBinding(ctx context.Context, wsStore *onibiworkspace.DBStore, entry onibiworkspace.IndexEntry) error {
	if err := saveWorkspaceIndex(entry); err != nil {
		return err
	}
	return wsStore.Upsert(ctx, onibiworkspace.DBEntry{
		Name:      entry.Name,
		Path:      entry.Path,
		SSHKeyRef: entry.SSHKey,
		LastSeen:  entry.LastSeen,
	})
}

func saveWorkspaceIndex(entry onibiworkspace.IndexEntry) error {
	dir, err := onibiworkspace.DefaultIndexDir()
	if err != nil {
		return err
	}
	return onibiworkspace.SaveIndexEntry(dir, entry)
}

func loadWorkspaceIndex(name string) (onibiworkspace.IndexEntry, bool) {
	dir, err := onibiworkspace.DefaultIndexDir()
	if err != nil {
		return onibiworkspace.IndexEntry{}, false
	}
	entry, err := onibiworkspace.LoadIndexEntry(dir, name)
	return entry, err == nil
}

func removeWorkspaceIndex(name string) (bool, error) {
	dir, err := onibiworkspace.DefaultIndexDir()
	if err != nil {
		return false, err
	}
	path, err := onibiworkspace.EntryPath(dir, name)
	if err != nil {
		return false, err
	}
	if err := os.Remove(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func projectDefaultTransport(root, name string) string {
	cfg, err := onibiworkspace.LoadProjectConfig(filepath.Join(root, onibiworkspace.ProjectRelPath))
	if err != nil || cfg.Name != name {
		return ""
	}
	return cfg.Transports.Default
}

func exportWorkspaceBundle(entry onibiworkspace.DBEntry, dstRoot string) error {
	sourceRoot := entry.Path
	if info, err := os.Stat(sourceRoot); err != nil {
		return err
	} else if !info.IsDir() {
		return fmt.Errorf("workspace path is not a directory: %s", sourceRoot)
	}
	onibiDir := bundleOnibiDir(dstRoot)
	if err := os.MkdirAll(onibiDir, 0o755); err != nil {
		return err
	}
	if err := exportProjectWorkspaceFile(entry, filepath.Join(onibiDir, "workspace.toml")); err != nil {
		return err
	}
	return nil
}

func exportProjectWorkspaceFile(entry onibiworkspace.DBEntry, dst string) error {
	src := filepath.Join(entry.Path, onibiworkspace.ProjectRelPath)
	cfg := onibiworkspace.ProjectConfig{SchemaVersion: 1, Name: entry.Name}
	if loaded, err := onibiworkspace.LoadProjectConfig(src); err == nil {
		cfg = loaded
		cfg.Name = entry.Name
	}
	if indexEntry, ok := loadWorkspaceIndex(entry.Name); ok && cfg.Transports.Default == "" {
		cfg.Transports.Default = indexEntry.DefaultTransport
	}
	return onibiworkspace.SaveProjectConfig(dst, cfg)
}

func projectFileFromArg(arg string) (onibiworkspace.ProjectFile, error) {
	path, err := filepath.Abs(strings.TrimSpace(arg))
	if err != nil {
		return onibiworkspace.ProjectFile{}, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return onibiworkspace.ProjectFile{}, err
	}
	if !info.IsDir() {
		if filepath.Base(path) != "workspace.toml" {
			return onibiworkspace.ProjectFile{}, fmt.Errorf("not a workspace file: %s", path)
		}
		return projectFileFromWorkspacePath(path), nil
	}
	if filepath.Base(path) == ".onibi" {
		workspacePath := filepath.Join(path, "workspace.toml")
		if _, err := os.Stat(workspacePath); err != nil {
			return onibiworkspace.ProjectFile{}, err
		}
		return onibiworkspace.ProjectFile{Root: filepath.Dir(path), Path: workspacePath}, nil
	}
	if _, err := os.Stat(filepath.Join(path, "workspace.toml")); err == nil {
		return onibiworkspace.ProjectFile{Root: filepath.Dir(path), Path: filepath.Join(path, "workspace.toml")}, nil
	}
	found, ok, err := onibiworkspace.FindProjectFile(path)
	if err != nil {
		return onibiworkspace.ProjectFile{}, err
	}
	if !ok {
		return onibiworkspace.ProjectFile{}, fmt.Errorf("workspace.toml not found under %s", path)
	}
	return found, nil
}

func projectFileFromWorkspacePath(path string) onibiworkspace.ProjectFile {
	dir := filepath.Dir(path)
	if filepath.Base(dir) == ".onibi" {
		return onibiworkspace.ProjectFile{Root: filepath.Dir(dir), Path: path}
	}
	return onibiworkspace.ProjectFile{Root: dir, Path: path}
}

func bundleOnibiDir(root string) string {
	if filepath.Base(root) == ".onibi" {
		return root
	}
	return filepath.Join(root, ".onibi")
}
