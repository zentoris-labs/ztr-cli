package auth

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/gofrs/flock"
)

// lockTimeout bounds how long a command waits to acquire a profile's refresh lock before giving
// up. A package var so tests can shorten it.
var lockTimeout = 10 * time.Second

// withProfileLock runs fn while holding an exclusive, cross-process lock for one profile, so that
// concurrent zentoris invocations cannot refresh (and rotate) the same login's tokens at the same
// time - a race that, with refresh-token rotation, can otherwise revoke the whole token chain. The
// lock is a separate file in the state dir, independent of where the credentials themselves live.
func withProfileLock(profile string, fn func() error) error {
	return withFileLock(filepath.Join(zentorisDir(), "credentials-"+normProfile(profile)+".lock"), fn)
}

// withStateLock serializes read-modify-write of the shared profile index (config.json) across
// processes, so concurrent logins / switches / logouts cannot clobber each other's index changes.
func withStateLock(fn func() error) error {
	return withFileLock(filepath.Join(zentorisDir(), stateFile+".lock"), fn)
}

// withFileLock runs fn while holding an exclusive, cross-process advisory lock on lockPath.
func withFileLock(lockPath string, fn func() error) error {
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o700); err != nil {
		return err
	}
	fl := flock.New(lockPath)

	ctx, cancel := context.WithTimeout(context.Background(), lockTimeout)
	defer cancel()
	locked, err := fl.TryLockContext(ctx, 50*time.Millisecond)
	if err != nil {
		return fmt.Errorf("acquire lock %s: %w", lockPath, err)
	}
	if !locked {
		return fmt.Errorf("acquire lock %s: timed out", lockPath)
	}
	defer func() { _ = fl.Unlock() }()

	return fn()
}
