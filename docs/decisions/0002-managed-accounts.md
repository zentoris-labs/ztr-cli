# 0002 - Managed accounts: switch, list, and an active default

- Status: Accepted
- Date: 2026-08-20
- Follows: [0001](0001-configuration-surface.md)

## Context

The `--account` flag (originally named `--profile`) already namespaced the stored `auth login`
session - a keychain entry / file per name - so multiple accounts could physically coexist. But
there was no way to *manage* them: no command to see which accounts existed, no persisted "active"
account (you had to repeat `--account` or export the env var every time), and the
`Credentials.Subject` field was never populated, so the CLI could not tell you which identity an
account held. It was a mechanism, not a feature.

We chose to complete it along the lines of `gh auth` (login / status / switch over a listable,
switchable active account) rather than leave it half-built or drop it. We also renamed the concept
from "profile" to "account": ours is a login identity, not an AWS-style freely-defined settings
bundle, so "account" (the gh term) describes it honestly.

## Decision

- **Active account is persisted** in `~/.zentoris/config.json` alongside an index of known accounts.
  OS keychains are not enumerable, so the CLI tracks known accounts itself (the same reason `gh`
  keeps a hosts file). The file holds only names and the active pointer - never tokens.
- **Resolution order for the account is** `--account` > `ZENTORIS_ACCOUNT` > the persisted active
  account > `default`. The flag layer resolves this after parsing; `internal/config` only reads the
  env var, because the persisted state lives in `internal/auth`, which `config` must not import.
- **`auth switch <account>`** sets the active account; it refuses an account that has no stored
  credentials, so you cannot switch to one you have not logged into.
- **`auth list`** enumerates known accounts (index unioned with any credential files on disk),
  marking the active one and showing each account's storage backend, captured identity, and whether
  its session has expired.
- **A fresh `auth login` becomes the active account**, and `auth logout` removes the account from
  the index (clearing active if it pointed there).
- **Identity is captured best-effort** at login by reading the `email` / `preferred_username` /
  `sub` claim from the access token when it is a JWT; opaque tokens simply leave the account
  unlabeled rather than failing. `auth status` displays it.
- **Accounts apply to `auth login` only.** A `--token`, client-credentials, or CI-OIDC credential
  is a single ambient identity and is unaffected by the account - documented so the scope is clear.

## Storage location

State moved to `~/.zentoris/` - a dotfolder in the home directory (the `~/.aws`, `~/.kube` model) -
instead of the OS user-config dir. One predictable path on every platform. It holds the file-backed
credential fallback (`credentials-<account>.json`, `0600`) and the account index (`config.json`).
Credentials still prefer the OS keychain; the file is only the fallback for hosts with no keychain
backend (headless Linux, most CI), and a keychain success deletes any stale file.

## Consequences

- Multi-account is now first-class and discoverable: log in several times, `auth list` to see them,
  `auth switch` to pick a default, `--account` to override per command.
- `~/.zentoris/config.json` is new state on disk; it holds only account names and the active one -
  never tokens, which stay in the keychain / `0600` credential files.
- Identity display depends on the token being a readable JWT; if the platform issues opaque access
  tokens, `auth status` / `auth list` show the account without an identity label. That is acceptable
  and self-correcting once identity is available.
