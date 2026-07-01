package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

const updateTimeout = 10 * time.Second

var (
	updateLatestURL   = "https://api.github.com/repos/gongahkia/onibi/releases/latest"
	updateReleasesURL = "https://api.github.com/repos/gongahkia/onibi/releases?per_page=1"
	updateHTTPClient  = http.DefaultClient
)

type updateRelease struct {
	TagName    string `json:"tag_name"`
	HTMLURL    string `json:"html_url"`
	Prerelease bool   `json:"prerelease"`
}

func runUpdate(cmd *cobra.Command, _ []string) error {
	channel, _ := cmd.Flags().GetString("channel")
	checkOnly, _ := cmd.Flags().GetBool("check-only")
	channel = strings.ToLower(strings.TrimSpace(channel))
	if channel == "" {
		channel = "stable"
	}
	if channel != "stable" && channel != "beta" {
		return errors.New("--channel must be stable or beta")
	}
	ctx, cancel := context.WithTimeout(cmd.Context(), updateTimeout)
	defer cancel()
	rel, err := fetchUpdateRelease(ctx, channel)
	if err != nil {
		return err
	}
	if checkOnly {
		fmt.Fprintf(cmd.OutOrStdout(), "latest %s: %s\n", channel, rel.TagName)
		if rel.HTMLURL != "" {
			fmt.Fprintf(cmd.OutOrStdout(), "url: %s\n", rel.HTMLURL)
		}
		return nil
	}
	return errors.New("update apply is not implemented yet; use --check-only")
}

func fetchUpdateRelease(ctx context.Context, channel string) (updateRelease, error) {
	switch channel {
	case "stable":
		return fetchStableRelease(ctx)
	case "beta":
		return fetchBetaRelease(ctx)
	default:
		return updateRelease{}, errors.New("unknown update channel")
	}
}

func fetchStableRelease(ctx context.Context) (updateRelease, error) {
	var rel updateRelease
	if err := getUpdateJSON(ctx, updateLatestURL, &rel); err != nil {
		return updateRelease{}, err
	}
	if strings.TrimSpace(rel.TagName) == "" {
		return updateRelease{}, errors.New("stable release missing tag_name")
	}
	return rel, nil
}

func fetchBetaRelease(ctx context.Context) (updateRelease, error) {
	var releases []updateRelease
	if err := getUpdateJSON(ctx, updateReleasesURL, &releases); err != nil {
		return updateRelease{}, err
	}
	for _, rel := range releases {
		if rel.Prerelease && strings.TrimSpace(rel.TagName) != "" {
			return rel, nil
		}
	}
	return updateRelease{}, errors.New("no beta prerelease found")
}

func getUpdateJSON(ctx context.Context, url string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "onibi-update")
	if token := updateGitHubToken(); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := updateHTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("fetch release: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("fetch release: HTTP %d", resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("parse release: %w", err)
	}
	return nil
}

func updateGitHubToken() string {
	if token := strings.TrimSpace(os.Getenv("ONIBI_GITHUB_TOKEN")); token != "" {
		return token
	}
	return strings.TrimSpace(os.Getenv("GITHUB_TOKEN"))
}
