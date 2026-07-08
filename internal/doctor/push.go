package doctor

import (
	"context"
	"fmt"

	"github.com/gongahkia/onibi/internal/web"
)

func Push(ctx context.Context, opts Options) Report {
	var checks []Check
	db, err := openStoreDB(opts.Paths.DBFile)
	if err != nil {
		return Report{Checks: []Check{pushCheck("push sqlite db", Fail, err.Error(), "onibi doctor --push")}}
	}
	defer db.Close()
	checks = append(checks, pushCheck("push sqlite db", Pass, opts.Paths.DBFile, ""))
	diag, err := web.VAPIDDiagnosticsForDB(ctx, db)
	if err != nil {
		checks = append(checks, pushCheck("push vapid key", Fail, err.Error(), "onibi push rotate"))
		return Report{Checks: checks}
	}
	if !diag.SecretPresent {
		checks = append(checks, pushCheck("push vapid key", Fail, "missing "+web.PushVAPIDSecretName, "onibi push rotate"))
	} else {
		checks = append(checks, pushCheck("push vapid key", Pass, web.PushVAPIDSecretName, ""))
	}
	switch {
	case !diag.SecretPresent:
		checks = append(checks, pushCheck("push public key", Fail, "cannot compare without keyring VAPID keypair", "onibi push rotate"))
	case !diag.SQLitePublicPresent:
		checks = append(checks, pushCheck("push public key", Fail, "missing SQLite public key", "onibi push rotate"))
	case !diag.PublicKeyMatches:
		checks = append(checks, pushCheck("push public key", Fail, "SQLite public key does not match keyring VAPID keypair", "onibi push rotate"))
	default:
		checks = append(checks, pushCheck("push public key", Pass, "keyring and SQLite public key match", ""))
	}
	if diag.LegacyPrivatePresent {
		checks = append(checks, pushCheck("push legacy private key", Warn, "legacy encrypted SQLite private key still present", "restart daemon or run onibi push rotate"))
	} else {
		checks = append(checks, pushCheck("push legacy private key", Pass, "absent", ""))
	}
	if diag.SubscriptionCount > 0 && (!diag.SecretPresent || !diag.PublicKeyMatches) {
		checks = append(checks, pushCheck("push subscriptions", Fail, fmt.Sprintf("%d subscription(s) cannot be validated against current VAPID key", diag.SubscriptionCount), "onibi push rotate"))
	} else {
		checks = append(checks, pushCheck("push subscriptions", Pass, fmt.Sprintf("%d subscription(s)", diag.SubscriptionCount), ""))
	}
	return Report{Checks: checks}
}

func pushCheck(name string, st Status, detail, next string) Check {
	c := Check{Name: name, Status: st, Detail: detail, Code: codeFor(name)}
	if st != Pass {
		c.Next = next
		c.Impact = "Web Push notifications may not reach subscribed devices."
		c.SafeFix = next
		c.ManualFix = "inspect VAPID secret storage and push_subscriptions rows"
		c.Retry = "onibi doctor --push"
		c.Blocks = []string{"web_push"}
	}
	return c
}
