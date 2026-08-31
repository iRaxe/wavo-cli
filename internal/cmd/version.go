package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/iRaxe/wavo-cli/internal/brand"
	"golang.org/x/mod/semver"
)

const maxReleaseResponseBytes = 1 << 20

// VersionCmd is `wavo version` — prints the CLI version. Equivalent to
// the `--version` flag, exposed as a subcommand for discoverability.
type VersionCmd struct {
	Check bool `help:"Check GitHub for a newer release."`
}

func (c *VersionCmd) Run(app *App) error {
	v := app.Version
	if v == "" {
		v = "dev"
	}
	if !c.Check {
		_, err := fmt.Fprintln(app.Printer.Writer, v)
		return err
	}

	latest, err := fetchLatestRelease(brand.LatestReleaseURL)
	if err != nil {
		return fmt.Errorf("check failed: %w", err)
	}

	_, err = fmt.Fprint(app.Printer.Writer, versionUpdateMessage(v, latest))
	return err
}

func versionUpdateMessage(installed, latest string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s\n", brand.Command, displayVersion(installed))
	fmt.Fprintf(&b, "Latest: %s\n\n", displayVersion(latest))
	switch {
	case installed == "dev":
		b.WriteString("Running a dev build.\n")
	case comparableVersion(installed) == "" || comparableVersion(latest) == "":
		b.WriteString("Unable to compare versions safely.\n")
	case semver.Compare(comparableVersion(latest), comparableVersion(installed)) <= 0:
		b.WriteString("Up to date.\n")
	default:
		b.WriteString("Update available.\n")
		fmt.Fprintf(&b, "  curl -fsSL %s | sh\n", brand.InstallURL)
	}
	return b.String()
}

func displayVersion(v string) string {
	if v == "dev" || v == "" {
		return v
	}
	if strings.HasPrefix(v, "v") {
		return v
	}
	return "v" + v
}

func fetchLatestRelease(url string) (string, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitHub API returned %s", resp.Status)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxReleaseResponseBytes+1))
	if err != nil {
		return "", fmt.Errorf("read GitHub release response: %w", err)
	}
	if len(body) > maxReleaseResponseBytes {
		return "", fmt.Errorf("GitHub release response exceeds %d bytes", maxReleaseResponseBytes)
	}

	var releases []struct {
		TagName string `json:"tag_name"`
	}
	if err := json.Unmarshal(body, &releases); err != nil {
		return "", fmt.Errorf("decode GitHub release response: %w", err)
	}
	if len(releases) == 0 || releases[0].TagName == "" {
		return "", fmt.Errorf("no Wavo release found")
	}
	if !semver.IsValid(releases[0].TagName) {
		return "", fmt.Errorf("invalid Wavo release tag")
	}
	return releases[0].TagName, nil
}

func comparableVersion(v string) string {
	if !strings.HasPrefix(v, "v") {
		v = "v" + v
	}
	if !semver.IsValid(v) {
		return ""
	}
	return v
}
