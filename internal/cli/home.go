package cli

import (
	"bufio"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/gongahkia/onibi/internal/brand"
)

func runRootLanding(cmd *cobra.Command, _ []string) error {
	printCLIHeader(cmd, "")
	style := styleFor(cmd)
	fmt.Fprintln(cmd.OutOrStdout(), style.bold("Onibi command center"))
	fmt.Fprintln(cmd.OutOrStdout(), style.dim("Private control for coding agents. No local secrets are opened here."))
	fmt.Fprintln(cmd.OutOrStdout())
	rows := [][]string{
		{"1", "Start cockpit", "guided first-run setup, then a phone pairing QR"},
		{"2", "Pair phone", "show a fresh QR for the local web cockpit"},
		{"3", "Connect agents", "install or inspect coding-agent hooks"},
		{"4", "Check system", "review diagnostics, security, service, and config"},
		{"5", "Telegram beta", "set up the owner-only chat cockpit"},
		{"6", "Workspace", "create portable project metadata"},
	}
	if err := renderMenuPanel(cmd, "WORKFLOWS", rows); err != nil {
		return err
	}
	if !shouldPromptPairTransport(cmd) {
		fmt.Fprintln(cmd.OutOrStdout(), style.dim("Run `onibi --help` for every command."))
		return nil
	}
	fmt.Fprint(cmd.OutOrStdout(), "Choose 1–6, or Enter to exit: ")
	sc := bufio.NewScanner(cmd.InOrStdin())
	if !sc.Scan() {
		return sc.Err()
	}
	choice := strings.TrimSpace(strings.ToLower(sc.Text()))
	if choice == "" || choice == "q" || choice == "quit" {
		return nil
	}
	routes := map[string][]string{
		"1": {"start"}, "2": {"phone", "pair"}, "3": {"agent", "install"},
		"4": {"system", "doctor"}, "5": {"telegram", "status"}, "6": {"workspace", "init"},
	}
	args, ok := routes[choice]
	if !ok {
		return fmt.Errorf("choose a number from 1 through 6")
	}
	cmd.Root().SetArgs(args)
	return cmd.Root().Execute()
}

func logoCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "logo",
		Short: "Print Onibi ASCII art",
		Run: func(cmd *cobra.Command, _ []string) {
			width, _ := cmd.Flags().GetInt("width")
			if width <= 0 {
				width = logoWidth(cmd)
			}
			if width > 0 {
				cmd.Println(brand.RenderExact(width))
				return
			}
			cmd.Println(brand.ANSIForWriterWidth(cmd.OutOrStdout(), 0))
		},
	}
	cmd.Flags().Int("width", 0, "render width")
	return cmd
}

func printCLIHeader(cmd *cobra.Command, title string) {
	if !showLogo(cmd) {
		if title != "" && !quiet(cmd) {
			fmt.Fprintln(cmd.OutOrStdout(), styleFor(cmd).bold(title))
			fmt.Fprintln(cmd.OutOrStdout())
		}
		return
	}
	fmt.Fprintln(cmd.OutOrStdout(), brand.ANSIForWriterWidth(cmd.OutOrStdout(), logoWidth(cmd)))
	if title == "" {
		fmt.Fprintln(cmd.OutOrStdout())
		return
	}
	fmt.Fprintln(cmd.OutOrStdout())
	fmt.Fprintln(cmd.OutOrStdout(), styleFor(cmd).bold(title))
	fmt.Fprintln(cmd.OutOrStdout())
}

func showLogo(cmd *cobra.Command) bool {
	return !quiet(cmd) && !boolFlag(cmd, "no-logo")
}

func quiet(cmd *cobra.Command) bool {
	return boolFlag(cmd, "quiet")
}

func debug(cmd *cobra.Command) bool {
	return boolFlag(cmd, "debug")
}

func commandLogLevel(cmd *cobra.Command) slog.Level {
	if debug(cmd) {
		return slog.LevelDebug
	}
	return slog.LevelWarn
}

func logoWidth(cmd *cobra.Command) int {
	return intFlag(cmd, "logo-width")
}

func boolFlag(cmd *cobra.Command, name string) bool {
	flag := cmd.Flag(name)
	if flag == nil && cmd.Root() != nil {
		flag = cmd.Root().Flag(name)
	}
	if flag == nil {
		return false
	}
	return flag.Value.String() == "true"
}

func intFlag(cmd *cobra.Command, name string) int {
	flag := cmd.Flag(name)
	if flag == nil && cmd.Root() != nil {
		flag = cmd.Root().Flag(name)
	}
	if flag == nil {
		return 0
	}
	n, _ := strconv.Atoi(flag.Value.String())
	return n
}
