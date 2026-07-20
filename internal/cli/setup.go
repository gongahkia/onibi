package cli

import (
	"bufio"
	"fmt"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/gongahkia/onibi/internal/config"
	"github.com/gongahkia/onibi/internal/doctor"
	"github.com/gongahkia/onibi/internal/service"
	"github.com/gongahkia/onibi/internal/store"
)

var doctorRun = doctor.Run
var inputIsTerminal = func(in any) bool {
	f, ok := in.(interface{ Fd() uintptr })
	return ok && term.IsTerminal(int(f.Fd()))
}
var outputIsTerminal = func(out any) bool {
	f, ok := out.(interface{ Fd() uintptr })
	return ok && term.IsTerminal(int(f.Fd()))
}

func runSetup(cmd *cobra.Command, _ []string) error {
	printChecklist, _ := cmd.Flags().GetBool("print-checklist")
	complete, _ := cmd.Flags().GetBool("complete")
	if printChecklist {
		printSetupChecklist(cmd.OutOrStdout())
		return nil
	}
	paths, err := config.DefaultPaths()
	if err != nil {
		return err
	}
	if err := paths.EnsureDirs(); err != nil {
		return err
	}
	db, err := openDefaultDB()
	if err != nil {
		return err
	}
	defer db.Close()
	printCLIHeader(cmd, "Setup")
	fmt.Fprintln(cmd.OutOrStdout(), "Onibi setup is CLI-first: start the cockpit, scan the QR, then install hooks.")
	fmt.Fprintln(cmd.OutOrStdout(), "The iPhone CA profile is printed by `onibi up` when local HTTPS is needed.")
	if complete {
		return runSetupComplete(cmd, paths, db)
	}
	printSetupNextActions(cmd)
	return nil
}

func runSetupComplete(cmd *cobra.Command, paths config.Paths, db *store.DB) error {
	br := bufio.NewReader(cmd.InOrStdin())
	if askYesNo(cmd, br, "Install and start background service? [Y/n] ", true) {
		m, err := service.NewManager(paths, "")
		if err != nil {
			return err
		}
		if err := m.Install(cmd.Context()); err != nil {
			return err
		}
		fmt.Fprintln(cmd.OutOrStdout(), "Service installed.")
	}
	if askYesNo(cmd, br, "Auto-detect and install agent hooks? [Y/n] ", true) {
		notifyBin, err := locateNotifyBinary()
		if err != nil {
			if err := handleMissingNotifyBinary(cmd, br, err); err != nil {
				return err
			}
		} else {
			if err := runInteractiveHooks(cmd, db, notifyBin, false); err != nil {
				return err
			}
		}
	}
	fmt.Fprintln(cmd.OutOrStdout(), "\nDoctor summary:")
	style := styleFor(cmd)
	report := doctorRun(cmd.Context(), doctor.Options{Paths: paths, Mode: "installed"})
	for _, c := range report.Checks {
		fmt.Fprintf(cmd.OutOrStdout(), "%s %s: %s\n", style.status(c.Status), c.Name, c.Detail)
	}
	if report.Failed() {
		return fmt.Errorf("setup complete but doctor failed")
	}
	printSetupNextActions(cmd)
	return nil
}

func printSetupNextActions(cmd *cobra.Command) {
	fmt.Fprintln(cmd.OutOrStdout(), "\nNext:")
	_ = renderTable(cmd.OutOrStdout(), [][]string{
		{"1", "onibi status", "inspect local state"},
		{"2", "onibi up", "choose category/provider, start control surface"},
		{"3", "onibi install-hooks --interactive", "connect agents"},
		{"4", "onibi hooks --show --all", "verify hook drift"},
		{"5", "onibi doctor --fix", "apply safe local fixes"},
	})
}

func handleMissingNotifyBinary(cmd *cobra.Command, br *bufio.Reader, cause error) error {
	fmt.Fprintln(cmd.ErrOrStderr(), "")
	fmt.Fprintln(cmd.ErrOrStderr(), "onibi-notify not found. Remediation:")
	fmt.Fprintln(cmd.ErrOrStderr(), "  1) make install")
	fmt.Fprintln(cmd.ErrOrStderr(), "  2) export ONIBI_NOTIFY_BIN=/abs/path/to/onibi-notify")
	fmt.Fprintln(cmd.ErrOrStderr(), "  3) onibi adapters")
	fmt.Fprintln(cmd.ErrOrStderr(), "  4) onibi install-hooks --interactive")
	if inputIsTerminal(cmd.InOrStdin()) && askYesNo(cmd, br, "Continue without hooks? [y/N] ", false) {
		return nil
	}
	return fmt.Errorf("hooks step aborted: onibi-notify missing: %w", cause)
}

func askYesNo(cmd *cobra.Command, br *bufio.Reader, prompt string, def bool) bool {
	fmt.Fprint(cmd.OutOrStdout(), prompt)
	line, _ := br.ReadString('\n')
	switch line {
	case "\n", "\r\n", "":
		return def
	case "y\n", "Y\n", "yes\n", "YES\n", "Yes\n":
		return true
	default:
		return false
	}
}

func printSetupChecklist(out interface{ Write([]byte) (int, error) }) {
	body := `Setup checklist:

  [ ] Onibi local CA profile installed only from your own onibi up output
  [ ] iPhone trusts the Onibi local CA when using Safari cockpit
  [ ] Hotspot available if managed Wi-Fi blocks peer traffic
  [ ] State dir 0700, socket 0600 (run: onibi doctor)
  [ ] All installed hook hashes match registry
`
	_, _ = out.Write([]byte(body))
}
