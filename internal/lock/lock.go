// Package lock provides advisory file locks for Wavo CLI mutations.
package lock

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/iRaxe/wavo-cli/internal/config"
)

// ErrLocked is returned when another process already holds the lock.
var ErrLocked = errors.New("resource is locked by another Wavo process")

// Lock is a held advisory lock. Release it with Release.
// The OS releases it automatically if the process exits or crashes.
type Lock struct {
	f *os.File
}

// AcquireConversation takes an exclusive non-blocking lock for the given
// conversation ID. It returns ErrLocked (wrapped) if another process holds it.
func AcquireConversation(id int) (*Lock, error) {
	cfgDir, err := config.ConfigDir()
	if err != nil {
		return nil, err
	}
	return acquireAt(filepath.Join(cfgDir, "locks"), id)
}

// AcquireAuth takes the active build profile's exclusive non-blocking lock for
// config and credential mutations.
func AcquireAuth() (*Lock, error) {
	cfgDir, err := config.ConfigDir()
	if err != nil {
		return nil, err
	}
	name := "auth.lock"
	if config.IsDev {
		name = "auth-dev.lock"
	}
	return acquireFileAt(filepath.Join(cfgDir, "locks"), name, "Wavo authentication")
}

func acquireAt(dir string, id int) (*Lock, error) {
	return acquireFileAt(dir, fmt.Sprintf("conv-%d.lock", id), fmt.Sprintf("conversation %d", id))
}

func acquireFileAt(dir, name, subject string) (*Lock, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, name)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := tryLock(f); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("%s: %w", subject, err)
	}
	// Best-effort PID marker for debugging; the flock is the real lock.
	_ = f.Truncate(0)
	_, _ = f.WriteString(strconv.Itoa(os.Getpid()) + "\n")
	return &Lock{f: f}, nil
}

// Release unlocks and closes the lock file. The file itself is left in place:
// unlinking it would race with other processes locking the same path.
func (l *Lock) Release() {
	if l == nil || l.f == nil {
		return
	}
	_ = unlock(l.f)
	_ = l.f.Close()
	l.f = nil
}
