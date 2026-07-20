package cli

import (
	"fmt"
	"log/slog"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/gongahkia/onibi/internal/brand"
)

func runRootLanding(cmd *cobra.Command, _ []string) error {
	printCLIHeader(cmd, "")
	style := styleFor(cmd)
	fmt.Fprintln(cmd.OutOrStdout(), style.bold("Onibi")+" - web cockpit for local coding agents")
	fmt.Fprintln(cmd.OutOrStdout())
	rows := [][]string{
		{"Start", "onibi up", "start the local web cockpit and print a pairing QR"},
		{"Cellular", "onibi up --transport=tailscale", "use Tailscale Funnel and print QR"},
		{"Pair", "onibi pair", "mint another phone QR"},
		{"Integrate", "onibi install-hooks --interactive", "install agent hooks"},
		{"Check", "onibi status", "show daemon, devices, hooks, and doctor summary"},
		{"Repair", "onibi doctor --fix", "apply safe local fixes"},
	}
	if err := renderTable(cmd.OutOrStdout(), rows); err != nil {
		return err
	}
	fmt.Fprintln(cmd.OutOrStdout())
	fmt.Fprintln(cmd.OutOrStdout(), style.dim("More: onibi --help | onibi quickstart | onibi logo"))
	return nil
}

func quickstartCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "quickstart",
		Aliases: []string{"intro", "getting-started"},
		Short:   "Print the first-run flow",
		RunE: func(cmd *cobra.Command, _ []string) error {
			printCLIHeader(cmd, "Quickstart")
			rows := [][]string{
				{"1", "onibi status", "inspect local state"},
				{"2", "onibi up", "start the web cockpit and pair a device"},
				{"3", "onibi install-hooks --interactive", "connect agents"},
				{"4", "onibi doctor --fix", "apply safe local fixes"},
			}
			return renderTable(cmd.OutOrStdout(), rows)
		},
	}
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
