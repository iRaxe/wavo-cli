package brandcheck

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

func TestScanSymlinkTargetsScansStoredAndCurrentTargets(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink fixture requires Unix symlink support")
	}
	repo := t.TempDir()
	runGit(t, repo, "init", "-q")

	cleanLink := "clean link with spaces"
	if err := os.Symlink("clean-target", filepath.Join(repo, cleanLink)); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", "--", cleanLink)

	forbidden := "Chat" + "woot"
	storedLink := "stored-target-link"
	if err := os.Symlink(forbidden+"-missing", filepath.Join(repo, storedLink)); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", "--", storedLink)

	currentLink := "current-target-link"
	if err := os.Symlink("initial-clean-target", filepath.Join(repo, currentLink)); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", "--", currentLink)
	if err := os.Remove(filepath.Join(repo, currentLink)); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(forbidden+"-current", filepath.Join(repo, currentLink)); err != nil {
		t.Fatal(err)
	}

	matches, err := ScanSymlinkTargets(context.Background(), repo)
	if err != nil {
		t.Fatalf("ScanSymlinkTargets() error = %v", err)
	}
	if len(matches) != 2 {
		t.Fatalf("ScanSymlinkTargets() matches = %#v, want stored and current matches", matches)
	}
	got := map[string]string{}
	for _, match := range matches {
		got[match.Path] = match.Source
	}
	if got[storedLink] != "stored" || got[currentLink] != "current" {
		t.Fatalf("ScanSymlinkTargets() matches = %#v, want stored and current sources", matches)
	}
}

func TestScanSymlinkTargetsRejectsNonRepository(t *testing.T) {
	if _, err := ScanSymlinkTargets(context.Background(), t.TempDir()); err == nil {
		t.Fatal("ScanSymlinkTargets() error = nil, want Git listing error")
	}
}

func TestScanSymlinkTargetsIgnoresCallerGitEnvironment(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink fixture requires Unix symlink support")
	}
	repo := t.TempDir()
	otherRepo := t.TempDir()
	runGit(t, repo, "init", "-q")
	runGit(t, otherRepo, "init", "-q")

	link := "real-repository-link"
	if err := os.Symlink("Chat"+"woot-target", filepath.Join(repo, link)); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", "--", link)

	t.Setenv("GIT_DIR", filepath.Join(otherRepo, ".git"))
	t.Setenv("GIT_WORK_TREE", otherRepo)
	t.Setenv("GIT_INDEX_FILE", filepath.Join(otherRepo, ".git", "index"))
	fakeTools := t.TempDir()
	fakeMarker := filepath.Join(fakeTools, "fake-git-invoked")
	fakeGit := "#!/bin/sh\n: >\"" + fakeMarker + "\"\nexit 99\n"
	if err := os.WriteFile(filepath.Join(fakeTools, "git"), []byte(fakeGit), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", fakeTools+string(os.PathListSeparator)+os.Getenv("PATH"))
	matches, err := ScanSymlinkTargets(context.Background(), repo)
	if err != nil {
		t.Fatalf("ScanSymlinkTargets() error = %v", err)
	}
	if len(matches) != 1 || matches[0].Path != link {
		t.Fatalf("ScanSymlinkTargets() matches = %#v, want real repository target", matches)
	}
	if _, err := os.Stat(fakeMarker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("caller PATH git was invoked; marker stat error = %v", err)
	}
}

func runGit(t *testing.T, repo string, args ...string) {
	t.Helper()
	gitExecutable, err := trustedGitExecutable()
	if err != nil {
		t.Fatal(err)
	}
	commandArgs := append([]string{"-C", repo}, args...)
	command := exec.Command(gitExecutable, commandArgs...)
	command.Env = sanitizedGitEnvironment()
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, output)
	}
}
