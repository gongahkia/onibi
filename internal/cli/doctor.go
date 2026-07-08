package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/gongahkia/onibi/internal/buildinfo"
	"github.com/gongahkia/onibi/internal/config"
	"github.com/gongahkia/onibi/internal/daemon"
	"github.com/gongahkia/onibi/internal/doctor"
	"github.com/gongahkia/onibi/internal/secrets"
	"github.com/gongahkia/onibi/internal/updatecheck"
)

func runDoctor(cmd *cobra.Command, _ []string) error {
	offline, _ := cmd.Flags().GetBool("offline")
	mode, _ := cmd.Flags().GetString("mode")
	transportMode, _ := cmd.Flags().GetString("transport")
	fix, _ := cmd.Flags().GetBool("fix")
	afterUpgrade, _ := cmd.Flags().GetBool("after-upgrade")
	release, _ := cmd.Flags().GetBool("release")
	asJSON, _ := cmd.Flags().GetBool("json")
	explain, _ := cmd.Flags().GetBool("explain")
	providers, _ := cmd.Flags().GetBool("providers")
	if mode == "release" {
		release = true
	}
	if release {
		mode = "release"
		afterUpgrade = true
	}
	if afterUpgrade && !release {
		offline = true
	}
	paths, err := config.DefaultPaths()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(cmd.Context())
	defer cancel()
	opts := doctor.Options{Paths: paths, Offline: offline, Mode: mode, Transport: transportMode, AfterUpgrade: afterUpgrade}
	if doctorOptionsHook != nil {
		doctorOptionsHook(&opts)
	}
	style := styleFor(cmd)
	if providers {
		report := doctor.Providers(ctx, opts)
		if asJSON {
			enc := json.NewEncoder(cmd.OutOrStdout())
			enc.SetIndent("", "  ")
			return enc.Encode(report)
		}
		return renderDoctorProviders(cmd, report, fix)
	}
	if fix {
		fixes := doctor.Fix(ctx, opts)
		for _, a := range fixes.Actions {
			fmt.Fprintf(cmd.OutOrStdout(), "%s %s\n", style.fix(true), a)
		}
		for _, err := range fixes.Errors {
			fmt.Fprintf(cmd.OutOrStdout(), "%s %v\n", style.fix(false), err)
		}
		if fixes.Failed() {
			return fmt.Errorf("doctor fix failed")
		}
	}
	report := doctor.Run(ctx, opts)
	if release {
		report = augmentReleaseDoctorReport(ctx, paths, report, !offline)
	}
	if asJSON {
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		if err := enc.Encode(report); err != nil {
			return err
		}
		if report.Failed() {
			return fmt.Errorf("doctor failed")
		}
		return nil
	}
	for _, c := range report.Checks {
		fmt.Fprintf(cmd.OutOrStdout(), "%s %s: %s", style.status(c.Status), c.Name, c.Detail)
		if c.Next != "" {
			fmt.Fprintf(cmd.OutOrStdout(), " %s=%s", style.dim("next"), c.Next)
		}
		fmt.Fprintln(cmd.OutOrStdout())
		if explain && c.Status != doctor.Pass {
			printExplainLine(cmd, "impact", c.Impact)
			printExplainLine(cmd, "safe fix", c.SafeFix)
			printExplainLine(cmd, "manual fix", c.ManualFix)
			printExplainLine(cmd, "files", strings.Join(c.FilesTouched, ", "))
			printExplainLine(cmd, "retry", c.Retry)
			printExplainLine(cmd, "blocks", strings.Join(c.Blocks, ", "))
		}
	}
	if report.Failed() {
		return fmt.Errorf("doctor failed")
	}
	return nil
}

func renderDoctorProviders(cmd *cobra.Command, report doctor.ProviderReport, showFix bool) error {
	style := styleFor(cmd)
	rows := [][]string{tableHeader(style, "PROVIDER", "CONFIGURED", "REACHABLE", "LAST_AUDIT", "DETAIL")}
	for _, row := range report.Providers {
		rows = append(rows, []string{
			row.Name,
			providerYesNo(style, row.Configured),
			providerReachable(style, row.Reachable),
			valueOrDash(row.LastAuditTimestamp),
			row.Detail,
		})
	}
	if err := renderTable(cmd.OutOrStdout(), rows); err != nil {
		return err
	}
	if !showFix {
		return nil
	}
	fixRows := [][]string{tableHeader(style, "PROVIDER", "SETUP")}
	for _, row := range report.Providers {
		if row.Configured || len(row.Fix) == 0 {
			continue
		}
		fixRows = append(fixRows, []string{row.Name, strings.Join(row.Fix, " ; ")})
	}
	if len(fixRows) == 1 {
		return nil
	}
	fmt.Fprintln(cmd.OutOrStdout())
	return renderTable(cmd.OutOrStdout(), fixRows)
}

func providerYesNo(style cliStyle, ok bool) string {
	if ok {
		return style.green("yes")
	}
	return style.dim("no")
}

func providerReachable(style cliStyle, v string) string {
	switch v {
	case doctor.ReachableYes:
		return style.green(v)
	case doctor.ReachableNo:
		return style.red(v)
	default:
		return style.dim(v)
	}
}

func printExplainLine(cmd *cobra.Command, label, value string) {
	if value == "" {
		value = "-"
	}
	fmt.Fprintf(cmd.OutOrStdout(), "       %s: %s\n", styleFor(cmd).dim(label), value)
}

var doctorOptionsHook func(*doctor.Options)

func augmentReleaseDoctorReport(ctx context.Context, paths config.Paths, report doctor.Report, checkGitHub bool) doctor.Report {
	res := updateCheckRun(ctx, updatecheck.Options{
		CurrentVersion: buildinfo.Version,
		CurrentCommit:  buildinfo.Commit,
		CheckGitHub:    checkGitHub,
		Timeout:        updateCheckTimeout,
	})
	checks := make([]doctor.Check, 0, len(report.Checks)+2)
	checks = append(checks, updateDoctorCheck(res), telegramOptionalDoctorCheck(ctx, paths))
	checks = append(checks, report.Checks...)
	report.Checks = checks
	return report
}

func updateDoctorCheck(res updatecheck.Result) doctor.Check {
	st := doctor.Fail
	if res.Status == updatecheck.StatusCurrent {
		st = doctor.Pass
	}
	c := doctor.Check{Name: "update check", Status: st, Detail: res.Detail, Code: "update_check"}
	if st != doctor.Pass {
		c.Next = valueOrDefault(res.Command, "onibi update-check")
		c.Impact = "Release may not be using the expected Onibi build."
		c.SafeFix = "run the printed update command and rerun onibi doctor --release"
		c.ManualFix = "verify installed binary version, source checkout, and release tag manually"
		c.Retry = "onibi doctor --release"
		c.Blocks = []string{"release"}
	}
	return c
}

func telegramOptionalDoctorCheck(ctx context.Context, paths config.Paths) doctor.Check {
	db, err := openDefaultDB()
	if err != nil {
		return doctor.Check{Name: "telegram optional", Status: doctor.Warn, Detail: err.Error(), Code: "telegram_optional", Next: "onibi telegram status"}
	}
	defer db.Close()
	st, err := openSecretStore(secrets.Options{EnvFallbackPath: paths.EnvFile})
	if err != nil {
		return doctor.Check{Name: "telegram optional", Status: doctor.Warn, Detail: err.Error(), Code: "telegram_optional", Next: "onibi telegram status"}
	}
	_, storedTokenOK, _ := st.Get(daemon.TelegramSecretBotToken)
	tokenOK := storedTokenOK || strings.TrimSpace(os.Getenv(telegramTokenEnv)) != ""
	_, ownerOK, _ := db.KVGetString(ctx, daemon.TelegramKVOwnerChatID)
	if !tokenOK && !ownerOK {
		return doctor.Check{Name: "telegram optional", Status: doctor.Pass, Detail: "not configured; optional", Code: "telegram_optional"}
	}
	if tokenOK && ownerOK {
		return doctor.Check{Name: "telegram optional", Status: doctor.Pass, Detail: "configured", Code: "telegram_optional"}
	}
	c := doctor.Check{Name: "telegram optional", Status: doctor.Warn, Detail: "partially configured", Code: "telegram_optional", Next: "onibi telegram status"}
	c.Impact = "Telegram transport may not start or pair cleanly."
	c.SafeFix = "run onibi telegram setup, then onibi up --transport=telegram and complete pairing"
	c.ManualFix = "inspect stored Telegram token and telegram.owner_chat_id in local state"
	c.Retry = "onibi doctor --release"
	c.Blocks = []string{"telegram"}
	return c
}

func valueOrDefault(v, fallback string) string {
	if strings.TrimSpace(v) != "" {
		return v
	}
	return fallback
}
