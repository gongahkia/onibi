package cli

import (
	"encoding/json"
	"github.com/gongahkia/onibi/internal/buildinfo"
	"github.com/spf13/cobra"
	"runtime"
)

func Root() *cobra.Command {
	root := &cobra.Command{Use: "onibi", Short: "Control Ghostty wherever text renders", SilenceUsage: true, SilenceErrors: true}
	root.PersistentFlags().Bool("plain", false, "disable terminal animation and ASCII art (also ONIBI_PLAIN=1)")
	root.PersistentFlags().Bool("debug", false, "print debug errors")
	root.AddCommand(webCmd(), startCmd(), sessionCmd(), systemCmd(), doctorCmd(), versionCmd(), completionCmd())
	return root
}
func DebugEnabled(root *cobra.Command) bool { v, _ := root.Flags().GetBool("debug"); return v }
func versionCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "version", Short: "Print this executable's version", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		asJSON, _ := cmd.Flags().GetBool("json")
		if asJSON {
			return json.NewEncoder(cmd.OutOrStdout()).Encode(map[string]string{"version": buildinfo.Version, "commit": buildinfo.Commit, "builtAt": buildinfo.Date, "go": runtime.Version(), "platform": runtime.GOOS + "/" + runtime.GOARCH})
		}
		cmd.Println(buildinfo.Version)
		return nil
	}}
	cmd.Flags().Bool("json", false, "include revision, build time, Go toolchain and platform")
	return cmd
}
func completionCmd() *cobra.Command {
	return &cobra.Command{Use: "completion [bash|zsh|fish]", Short: "Generate shell completion", ValidArgs: []string{"bash", "zsh", "fish"}, Args: cobra.MatchAll(cobra.ExactArgs(1), cobra.OnlyValidArgs), RunE: func(cmd *cobra.Command, args []string) error {
		switch args[0] {
		case "bash":
			return cmd.Root().GenBashCompletion(cmd.OutOrStdout())
		case "zsh":
			return cmd.Root().GenZshCompletion(cmd.OutOrStdout())
		case "fish":
			return cmd.Root().GenFishCompletion(cmd.OutOrStdout(), true)
		default:
			return cobra.OnlyValidArgs(cmd, args)
		}
	}}
}
