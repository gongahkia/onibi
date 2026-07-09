package cli

import (
	"github.com/gongahkia/onibi/internal/config"
	"github.com/gongahkia/onibi/internal/daemon"
)

func daemonProviderOutputPolicy(cfg config.Config) daemon.ProviderOutputPolicy {
	return daemon.ProviderOutputPolicy{
		MaxChunks: cfg.Provider.Output.MaxChunks,
		MaxBytes:  cfg.Provider.Output.MaxBytes,
		Redaction: cfg.Provider.Output.Redaction,
	}
}

func daemonProviderOutputOverrides(cfg config.Config) daemon.ProviderOutputOverrides {
	return daemon.ProviderOutputOverrides{
		Telegram: daemonProviderOutputOverride(cfg.Provider.Output.Telegram),
		Matrix:   daemonProviderOutputOverride(cfg.Provider.Output.Matrix),
		Slack:    daemonProviderOutputOverride(cfg.Provider.Output.Slack),
		Discord:  daemonProviderOutputOverride(cfg.Provider.Output.Discord),
		Zulip:    daemonProviderOutputOverride(cfg.Provider.Output.Zulip),
		IRC:      daemonProviderOutputOverride(cfg.Provider.Output.IRC),
		Notify:   daemonProviderOutputOverride(cfg.Provider.Output.Notify),
	}
}

func daemonProviderOutputOverride(ov config.ProviderOutputOverride) daemon.ProviderOutputPolicy {
	return daemon.ProviderOutputPolicy{
		MaxChunks: ov.MaxChunks,
		MaxBytes:  ov.MaxBytes,
		Redaction: ov.Redaction,
	}
}
