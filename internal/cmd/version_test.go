package cmd

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFetchLatestReleaseIncludesPrereleases(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"tag_name":"v0.1.0-beta.2","prerelease":true},{"tag_name":"v0.1.0","prerelease":false}]`))
	}))
	defer server.Close()

	got, err := fetchLatestRelease(server.URL)
	if err != nil || got != "v0.1.0-beta.2" {
		t.Fatalf("fetchLatestRelease = (%q,%v)", got, err)
	}
}

func TestFetchLatestReleaseRejectsInvalidResponses(t *testing.T) {
	tests := map[string]struct {
		status int
		body   string
		want   string
	}{
		"http error": {status: http.StatusBadGateway, body: `private upstream detail`, want: "GitHub API returned 502 Bad Gateway"},
		"empty list": {status: http.StatusOK, body: `[]`, want: "no Wavo release found"},
		"empty tag":  {status: http.StatusOK, body: `[{"tag_name":""}]`, want: "no Wavo release found"},
		"oversize":   {status: http.StatusOK, body: `[{"tag_name":"v1"}]` + strings.Repeat(" ", maxReleaseResponseBytes), want: "response exceeds"},
		"unsafe tag": {status: http.StatusOK, body: `[{"tag_name":"v0.1.0/../../payload"}]`, want: "invalid Wavo release tag"},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = fmt.Fprint(w, tc.body)
			}))
			defer server.Close()

			_, err := fetchLatestRelease(server.URL)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("fetchLatestRelease error = %v, want substring %q", err, tc.want)
			}
			if strings.Contains(err.Error(), "private upstream detail") {
				t.Fatalf("error leaked response body: %v", err)
			}
		})
	}
}

func TestVersionUpdateMessageUsesWavoInstaller(t *testing.T) {
	got := versionUpdateMessage("v0.1.0-beta.1", "v0.1.0-beta.2")
	if !strings.Contains(got, "wavo v0.1.0-beta.1") ||
		!strings.Contains(got, "https://my.wavo.it/install-cli") {
		t.Fatalf("unexpected update output: %s", got)
	}
}

func TestVersionUpdateMessageUsesSemverPrecedence(t *testing.T) {
	tests := map[string]struct {
		installed string
		latest    string
		want      string
		dontWant  string
	}{
		"new prerelease":          {installed: "v0.1.0-beta.1", latest: "v0.1.0-beta.2", want: "Update available."},
		"stable beats prerelease": {installed: "v0.1.0-beta.2", latest: "v0.1.0", want: "Update available."},
		"installed newer":         {installed: "v0.2.0", latest: "v0.1.0", want: "Up to date.", dontWant: "Update available."},
		"build metadata equal":    {installed: "v0.1.0+local", latest: "v0.1.0+release", want: "Up to date.", dontWant: "Update available."},
		"numeric prerelease":      {installed: "v1.0.0-rc.9", latest: "v1.0.0-rc.10", want: "Update available."},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got := versionUpdateMessage(tc.installed, tc.latest)
			if !strings.Contains(got, tc.want) || tc.dontWant != "" && strings.Contains(got, tc.dontWant) {
				t.Fatalf("versionUpdateMessage(%q, %q) = %q", tc.installed, tc.latest, got)
			}
		})
	}
}
