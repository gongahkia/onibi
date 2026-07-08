package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/gongahkia/onibi/internal/buildinfo"
	"github.com/gongahkia/onibi/internal/store"
	"github.com/gongahkia/onibi/internal/updatecheck"
)

const updateCheckTimeout = 2 * time.Second
const updateCheckCacheKey = "cli.update_check.cache"
const updateCheckCacheTTL = 6 * time.Hour

var updateCheckRun = updatecheck.Check

func runUpdateCheck(cmd *cobra.Command, _ []string) error {
	repo, _ := cmd.Flags().GetString("repo")
	noGitHub, _ := cmd.Flags().GetBool("no-github")
	asJSON, _ := cmd.Flags().GetBool("json")
	ctx, cancel := context.WithTimeout(cmd.Context(), updateCheckTimeout)
	defer cancel()
	var cached *updatecheck.Result
	if !noGitHub && repo == "" {
		if _, db, err := openCLIStore(); err == nil {
			if res, ok := readUpdateCheckCache(ctx, db); ok && updateCacheMatchesBuild(res) {
				cached = &res
			}
			_ = db.Close()
		}
	}
	res := updateCheckRun(ctx, updatecheck.Options{
		CurrentVersion:          buildinfo.Version,
		CurrentCommit:           buildinfo.Commit,
		RepoDir:                 repo,
		CheckGitHub:             !noGitHub,
		Timeout:                 updateCheckTimeout,
		CachedResult:            cached,
		ConditionalETag:         cacheField(cached, "etag"),
		ConditionalLastModified: cacheField(cached, "last_modified"),
	})
	res = normalizeUpdateResult(res)
	cacheUpdateCheck(cmd.Context(), res)
	if asJSON {
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(res)
	}
	switch res.Status {
	case updatecheck.StatusOutdated:
		fmt.Fprintf(cmd.OutOrStdout(), "update available: %s\n", res.Detail)
	case updatecheck.StatusCurrent:
		fmt.Fprintf(cmd.OutOrStdout(), "up to date: %s\n", res.Detail)
	default:
		fmt.Fprintf(cmd.OutOrStdout(), "update check unavailable: %s\n", res.Detail)
	}
	if res.Command != "" {
		fmt.Fprintf(cmd.OutOrStdout(), "next: %s\n", res.Command)
	}
	return nil
}

func cachedUpdateCheck(ctx context.Context, db *store.DB, refresh bool) updatecheck.Result {
	var cached *updatecheck.Result
	if !refresh && db != nil {
		if res, ok := readUpdateCheckCache(ctx, db); ok && updateCacheMatchesBuild(res) {
			return res
		}
	}
	if db != nil {
		if res, ok := readUpdateCheckCache(ctx, db); ok && updateCacheMatchesBuild(res) {
			cached = &res
		}
	}
	res := updateCheckRun(ctx, updatecheck.Options{
		CurrentVersion:          buildinfo.Version,
		CurrentCommit:           buildinfo.Commit,
		CheckGitHub:             true,
		Timeout:                 updateCheckTimeout,
		CachedResult:            cached,
		ConditionalETag:         cacheField(cached, "etag"),
		ConditionalLastModified: cacheField(cached, "last_modified"),
	})
	res = normalizeUpdateResult(res)
	if db != nil {
		writeUpdateCheckCache(ctx, db, res)
	}
	return res
}

func cacheUpdateCheck(ctx context.Context, res updatecheck.Result) {
	_, db, err := openCLIStore()
	if err != nil {
		return
	}
	defer db.Close()
	writeUpdateCheckCache(ctx, db, res)
}

func writeUpdateCheckCache(ctx context.Context, db *store.DB, res updatecheck.Result) {
	res = normalizeUpdateResult(res)
	b, err := json.Marshal(res)
	if err != nil {
		return
	}
	_ = db.KVSet(ctx, updateCheckCacheKey, b, time.Now().Add(updateCheckCacheTTL).Unix())
}

func readUpdateCheckCache(ctx context.Context, db *store.DB) (updatecheck.Result, bool) {
	b, ok, err := db.KVGet(ctx, updateCheckCacheKey)
	if err != nil || !ok {
		return updatecheck.Result{}, false
	}
	var res updatecheck.Result
	if json.Unmarshal(b, &res) == nil && res.Status != "" {
		return normalizeUpdateResult(res), true
	}
	return updatecheck.Result{}, false
}

func normalizeUpdateResult(res updatecheck.Result) updatecheck.Result {
	if res.SchemaVersion == "" {
		res.SchemaVersion = updatecheck.SchemaVersion
	}
	if res.CurrentVersion == "" {
		res.CurrentVersion = buildinfo.Version
	}
	if res.CurrentCommit == "" {
		res.CurrentCommit = buildinfo.Commit
	}
	return res
}

func updateCacheMatchesBuild(res updatecheck.Result) bool {
	return res.CurrentVersion == buildinfo.Version && res.CurrentCommit == buildinfo.Commit
}

func cacheField(res *updatecheck.Result, field string) string {
	if res == nil {
		return ""
	}
	switch field {
	case "etag":
		return res.ETag
	case "last_modified":
		return res.LastModified
	default:
		return ""
	}
}
