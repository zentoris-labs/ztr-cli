# 0001 - Configuration surface: one `--domain` knob, fixed identity constants

- Status: Accepted
- Date: 2026-08-20

## Context

`zentoris` is pre-1.0 and not yet published. A flag or environment variable is a public
promise: once people script against it, removing it is a breaking change. Adding a knob later
is cheap; taking one away is not. So before the first release we deliberately trimmed the
surface to only what earns its place, and wrote down why.

## Decision

1. **One deployment knob: `--domain` (`ZENTORIS_DOMAIN`).** The main-API and auth/OP base URLs
   are derived from it as `main.api.<domain>` and `auth.api.<domain>`; they are never set
   directly. Dropped the earlier `--api` / `--auth-url` pair - two full URLs that had to agree
   and be kept in sync when a single base domain determines both.

2. **Identity is fixed, not configured.** The OAuth `client_id` (`cli`), the login `scope`, and
   the OP `tenant` (`main`) are constants in `internal/auth`, not flags or env. This is
   first-party tooling: like a browser app hardcoding its own client id, the CLI ships with its
   own identity. Only the deployment (`--domain`) varies per invocation. Dropped `--tenant`.

3. **JSON output only, no format flag.** The CLI emits indented JSON. `--output`/`-o` was
   removed because a "table" formatter did not exist, so the flag's advertised default did not
   match behavior. Re-introduce an output flag when a second format is actually implemented.

4. **Dropped `--resource` (RFC 8707 resource indicator).** It selects a token audience, which
   only matters when an OP issues tokens for multiple APIs. This CLI talks to one API and login
   tokens are platform-audienced by default, so the flag was a no-op in practice. Re-add it if a
   multi-audience deployment ever needs it.

5. **`--if-match` is pass-through.** An empty `--if-match` sends no precondition (last-write-wins);
   the caller passes an ETag for true optimistic concurrency, or `--if-match '*'` to overwrite
   unconditionally. The previous behavior silently substituted `*` for an empty value, which
   looked like concurrency control but provided none.

6. **Kept the named-login concept, called a profile** (`--profile` / `ZENTORIS_PROFILE`). Each
   profile is a separately-stored login (its own keychain entry / file), so several logins can
   coexist and a command runs as the chosen one; `default` is used when unset. The name follows the
   AWS CLI: a profile is a free-form label you choose, not a fixed account id - the account identity
   is captured from the token and shown separately. Built out into a managed feature (`auth list` /
   `auth switch`, a persisted active profile, captured identity) - see [0002](0002-managed-profiles.md).

## Consequences

- The public surface is `--domain`, `--token`, `--profile`, `--insecure` (root), plus per-command
  flags. Fewer knobs to document, learn, and keep honest.
- Endpoint and identity resolution live in one place (`internal/config`, `internal/auth`), so
  there is no split state to keep consistent.
- If a future need appears (a non-derivable auth host, a second output format, multi-audience
  tokens, per-invocation tenants), the removed knob is re-addable - that direction is the easy one.
