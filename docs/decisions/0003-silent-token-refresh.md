# 0003 - Silent token refresh with a per-profile lock

- Status: Accepted
- Date: 2026-08-20
- Follows: [0002](0002-managed-profiles.md)

## Context

Access tokens are short-lived (minutes to an hour). The login source stored a refresh token
(we request `offline_access` at sign-in) but never used it: on expiry it simply reported "no
credential", so the user had to run `auth login` again - unworkable for interactive or scripted
use where commands run continuously.

The token is resolved on **every** command (`api.Do` -> `resolver.Token`), and only the winning
credential source does anything, so refresh is a concern for the **login** source alone:

- `--token` is a static bearer - nothing to refresh.
- client-credentials already re-mints statelessly from its in-memory cache (no shared state).
- OIDC federation re-mints per command.
- **login** is the only source with a stored refresh token to renew.

## Decision

- **Renew silently before each command.** In the login source, if the access token has expired
  (or is within a 60s skew), exchange the stored refresh token for a fresh one (RFC 6749
  `refresh_token` grant), persist it, and continue. Sign in once; re-authenticate only when the
  refresh token itself expires or is revoked.
- **Guard concurrent refresh with a per-profile cross-process lock.** Parallel invocations share
  one profile.s stored credentials; because OPs commonly *rotate* refresh tokens, two processes
  refreshing at once can trigger refresh-token-reuse detection and revoke the whole chain. The
  renew path therefore runs under an exclusive file lock (`~/.zentoris/credentials-<profile>.lock`)
  with a **check-lock-check**: re-read inside the lock and reuse a token another process just wrote
  rather than refreshing again. The lock is a separate file, so it works whether credentials live
  in the keychain or a file.
- **Use `github.com/gofrs/flock` for the lock.** A small, well-established cross-platform advisory
  file lock (`flock` on Unix, `LockFileEx` on Windows). Chosen over a hand-rolled `O_EXCL` lockfile
  (stale-lock handling is subtle and easy to get wrong) - correctness of the locking is the whole
  point of this change.

## Consequences

- The failure modes of a refresh (rejected/expired refresh token, lock timeout, transient
  token-endpoint error) all yield "no credential", so the resolver falls through and, if nothing
  else authenticates, the user is told to sign in again. A transient network error during refresh
  therefore reads as "not logged in" rather than surfacing the underlying error - acceptable for
  now; a future refinement could distinguish a hard `invalid_grant` from a transient failure.
- New dependency: `github.com/gofrs/flock` (and its `golang.org/x/sys`). The first third-party
  dependency added for a runtime concern beyond the CLI framework and keychain; justified by the
  chain-revocation hazard the lock prevents.
- The refresh does one discovery round-trip per renewal (not per command - only when the token is
  actually stale). Fine at refresh frequency; discovery caching is a possible later optimization.
