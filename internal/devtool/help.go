package devtool

import (
	"github.com/gongahkia/onibi/internal/termui"
	"os"
)

func (t *Tool) Help() {
	if t.UI == nil {
		t.UI = termui.New(os.Stderr)
	}
	ui := t.UI
	originalWidth := ui.LabelWidth
	ui.LabelWidth = 20
	defer func() { ui.LabelWidth = originalWidth }()
	ui.Banner("Control Ghostty wherever text renders")
	ui.Section("START HERE")
	ui.Field("make run", "Build and start the web cockpit")
	ui.Field("make build", "Native build with elapsed progress; bin/build.log")
	ui.Field("make doctor", "Inspect passive Mac readiness")
	ui.Note("Keep the web terminal open. Use a second terminal for status or logs.")
	ui.Section("SOURCE INSTALLATION")
	for _, entry := range [][2]string{{"make install", "Install into ~/.local/bin"}, {"make install-status", "Verify revision and checksums"}, {"make update", "Rebuild this checkout; preserve state"}, {"make uninstall", "Remove owned binaries and login services"}} {
		ui.Field(entry[0], entry[1])
	}
	ui.Line("")
	for _, entry := range [][2]string{{"DRY_RUN=1", "Preview install/update/uninstall"}, {"UPDATE_PULL=1", "Fetch and fast-forward clean main during update"}, {"RESTART_WEB=1", "Explicitly restart web service after update"}, {"INSTALL_DIR=...", "Choose a user binary directory"}, {"VERBOSE=1", "Stream compiler output"}, {"ONIBI_PLAIN=1", "Disable animation and art"}, {"NO_COLOR=1", "Disable ANSI shading"}} {
		ui.Field(entry[0], entry[1])
	}
	ui.Section("CHECKS")
	ui.Field("make test", "Hermetic Go tests with race detection")
	ui.Field("make test-browser", "Browser/HTTP integration (npm ci first)")
	ui.Line("")
	ui.Note("Telegram: telegram-setup, telegram-doctor, telegram-run, telegram-dogfood")
}
