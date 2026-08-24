# 0002 - Managed profiles: switch, list, and an active default

- Status: Accepted
- Date: 2026-08-20
- Follows: [0001](0001-configuration-surface.md)

## Context

`--profile` already namespaced the stored `auth login` session - a keychain entry / file per name -
so multiple logins could physically coexist. But there was no way to *manage* them: no command to
see which profiles existed, no persisted "active" profile (you had to repeat `--profile` or export
the env var every time), and the `Credentials.Subject` field was never populated, so the CLI could
not tell you which account identity a profile held. It was a mechanism, not a feature.

We completed it along the lines of `gh auth` (login / status / switch over a listable, switchable
active login).

## Naming

We kept **profile** (the AWS CLI's term), not "account". A profile is a **free-form label the user
chooses** for a stored login - it is not a fixed account id and the CLI does not derive it from one.
The **account identity** (email / subject) is a separate thing: captured from the sign-in token and
shown next to the profile in `auth status` / `auth list`. So `auth status` reads
`Active profile: work` and `Account: you@example.com`. ("account" was considered for the label
itself and rejected precisely because the label is user-chosen, not the account.)

## Decision

- **Active profile is persisted** in `~/.zentoris/config.json` alongside an index of known profiles.
  OS keychains are not enumerable, so the CLI tracks known profiles itself (the same reason `gh`
  keeps a hosts file). The file holds only names and the active pointer - never tokens.
- **Resolution order for the profile is** `--profile` > `ZENTORIS_PROFILE` > the persisted active
  profile > `default`. The flag layer resolves this after parsing; `internal/config` only reads the
  env var, because the persisted active profile lives in `internal/auth`, which `config` must not
  import.
- **`auth switch <profile>`** sets the active profile; it refuses a profile that has no stored
  credentials, so you cannot switch to one you have not logged into.
- **`auth list`** enumerates known profiles (index unioned with any credential files on disk),
  marking the active one and showing each profile's storage backend, account identity, and whether
  its session has expired.
- **A fresh `auth login` becomes the active profile**, and `auth logout` removes the profile from
  the index (clearing active if it pointed there).
- **Account identity is captured best-effort** at login by reading the `email` /
  `preferred_username` / `sub` claim from the access token when it is a JWT; opaque tokens simply
  leave the profile unlabeled rather than failing. `auth status` displays it.
- **Profiles apply to `auth login` only.** A `--token`, client-credentials, or CI-OIDC credential
  is a single ambient identity and is unaffected by the profile - documented so the scope is clear.

## Storage location

State lives in `~/.zentoris/` - a dotfolder in the home directory (the `~/.aws`, `~/.kube` model).
It holds the file-backed credential fallback (`credentials-<profile>.json`, `0600`) and the profile
index (`config.json`). Credentials still prefer the OS keychain; the file is only the fallback for
hosts with no keychain backend, and a keychain success deletes any stale file.

## Consequences

- Multi-login is now first-class and discoverable: log in several times, `auth list` to see them,
  `auth switch` to pick a default, `--profile` to override per command.
- `~/.zentoris/config.json` is new state on disk; it holds only profile names and the active one -
  never tokens, which stay in the keychain / `0600` credential files.
- Identity display depends on the token being a readable JWT; if the platform issues opaque access
  tokens, `auth status` / `auth list` show the profile without an account label. That is acceptable
  and self-correcting once identity is available.
