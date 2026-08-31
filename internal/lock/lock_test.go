package lock

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/iRaxe/wavo-cli/internal/brand"
	"github.com/iRaxe/wavo-cli/internal/config"
)

func TestAcquireAuthConflictAndRelease(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	first, err := AcquireAuth()
	if err != nil {
		t.Fatalf("first AcquireAuth: %v", err)
	}
	if _, err := AcquireAuth(); !errors.Is(err, ErrLocked) || !strings.Contains(err.Error(), "Wavo") {
		t.Fatalf("second AcquireAuth error = %v, want Wavo-branded ErrLocked", err)
	}

	name := "auth.lock"
	if config.IsDev {
		name = "auth-dev.lock"
	}
	path := filepath.Join(home, brand.ConfigDirName, "locks", name)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("auth lock file %q missing: %v", path, err)
	}

	first.Release()
	second, err := AcquireAuth()
	if err != nil {
		t.Fatalf("AcquireAuth after release: %v", err)
	}
	second.Release()
}

func TestAcquireConflictAndRelease(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "locks")

	l1, err := acquireAt(dir, 42)
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}

	if _, err := acquireAt(dir, 42); !errors.Is(err, ErrLocked) {
		t.Fatalf("second acquire: want ErrLocked, got %v", err)
	}

	// A different conversation is unaffected.
	l2, err := acquireAt(dir, 43)
	if err != nil {
		t.Fatalf("acquire other conversation: %v", err)
	}
	l2.Release()

	l1.Release()
	l3, err := acquireAt(dir, 42)
	if err != nil {
		t.Fatalf("reacquire after release: %v", err)
	}
	l3.Release()
}

func TestReleaseIsIdempotent(t *testing.T) {
	l, err := acquireAt(filepath.Join(t.TempDir(), "locks"), 1)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	l.Release()
	l.Release() // must not panic
	var nilLock *Lock
	nilLock.Release() // nil receiver must be safe
}
