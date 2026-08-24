package auth

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/gofrs/flock"
)

// lockTimeout bounds how long a command waits to acquire an account's refresh lock before giving
// up. A package var so tests can shorten it.
var lockTimeout = 10 * time.Second

// withAccountLock runs fn while holding an exclusive, cross-process lock for one account, so that
// concurrent zentoris invocations cannot refresh (and rotate) the same login's tokens at the same
// time - a race that, with refresh-token rotation, can otherwise revoke the whole token chain. The
// lock is a separate file in the state dir, independent of where the credentials themselves live.
func withAccountLock(account string, fn func() error) error {
	if err := os.MkdirAll(zentorisDir(), 0o700); err != nil {
		return err
	}
	lockPath := filepath.Join(zentorisDir(), "credentials-"+normAccount(account)+".lock")
	fl := flock.New(lockPath)

	ctx, cancel := context.WithTimeout(context.Background(), lockTimeout)
	defer cancel()
	locked, err := fl.TryLockContext(ctx, 50*time.Millisecond)
	if err != nil {
		return fmt.Errorf("acquire account lock %s: %w", lockPath, err)
	}
	if !locked {
		return fmt.Errorf("acquire account lock %s: timed out", lockPath)
	}
	defer func() { _ = fl.Unlock() }()

	return fn()
}
