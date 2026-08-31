package brandcheck

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// SymlinkMatch identifies forbidden branding in either the stored Git target
// or the current worktree target of a tracked symlink.
type SymlinkMatch struct {
	Path   string
	Source string
	Target string
}

// ScanSymlinkTargets scans Git mode 120000 entries without following links.
// Git's NUL-delimited index output preserves paths containing whitespace.
func ScanSymlinkTargets(ctx context.Context, repoRoot string) ([]SymlinkMatch, error) {
	indexOutput, err := gitOutput(ctx, repoRoot, "ls-files", "-s", "-z")
	if err != nil {
		return nil, fmt.Errorf("list tracked files: %w", err)
	}

	var matches []SymlinkMatch
	for _, record := range bytes.Split(indexOutput, []byte{0}) {
		if len(record) == 0 {
			continue
		}
		header, pathBytes, ok := bytes.Cut(record, []byte{'\t'})
		if !ok {
			return nil, fmt.Errorf("parse tracked file record without path separator")
		}
		fields := bytes.Fields(header)
		if len(fields) != 3 {
			return nil, fmt.Errorf("parse tracked file record with %d header fields", len(fields))
		}
		if string(fields[0]) != "120000" {
			continue
		}

		path := string(pathBytes)
		storedTargetBytes, err := gitOutput(ctx, repoRoot, "cat-file", "blob", string(fields[1]))
		if err != nil {
			return nil, fmt.Errorf("read stored symlink target %q: %w", path, err)
		}
		storedTarget := string(storedTargetBytes)
		if containsForbiddenIdentity(storedTarget) {
			matches = append(matches, SymlinkMatch{Path: path, Source: "stored", Target: storedTarget})
		}

		worktreePath := filepath.Join(repoRoot, filepath.FromSlash(path))
		info, err := os.Lstat(worktreePath)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("inspect current symlink %q: %w", path, err)
		}
		if info.Mode()&os.ModeSymlink == 0 {
			continue
		}
		currentTarget, err := os.Readlink(worktreePath)
		if err != nil {
			return nil, fmt.Errorf("read current symlink target %q: %w", path, err)
		}
		if currentTarget != storedTarget && containsForbiddenIdentity(currentTarget) {
			matches = append(matches, SymlinkMatch{Path: path, Source: "current", Target: currentTarget})
		}
	}
	return matches, nil
}

func gitOutput(ctx context.Context, repoRoot string, args ...string) ([]byte, error) {
	gitExecutable, err := trustedGitExecutable()
	if err != nil {
		return nil, err
	}
	commandArgs := append([]string{"-C", repoRoot}, args...)
	command := exec.CommandContext(ctx, gitExecutable, commandArgs...)
	command.Env = sanitizedGitEnvironment()
	output, err := command.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return output, nil
}

func trustedGitExecutable() (string, error) {
	for _, candidate := range []string{"/usr/bin/git", "/bin/git"} {
		info, err := os.Stat(candidate)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return "", fmt.Errorf("inspect trusted Git candidate %q: %w", candidate, err)
		}
		if info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0 {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("git is not installed in a trusted system location")
}

func sanitizedGitEnvironment() []string {
	return []string{
		"PATH=/usr/bin:/bin",
		"LC_ALL=C",
	}
}

func containsForbiddenIdentity(value string) bool {
	lower := strings.ToLower(value)
	return strings.Contains(lower, "chat"+"woot") || strings.Contains(lower, "chwt"+".app")
}
