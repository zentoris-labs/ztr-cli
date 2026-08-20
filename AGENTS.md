# AGENTS.md - ztr-cli project instructions

Project instructions for every coding agent operating in this repository (Claude Code,
Cursor, Copilot, Codex, Aider, and any other tool that reads `AGENTS.md`). Vendor-neutral
by design. `CLAUDE.md` is a thin `@AGENTS.md` import for Claude Code (which loads
`CLAUDE.md`, not `AGENTS.md`) - keep rules HERE, never in `CLAUDE.md`.

## What this is

`zentoris` is the public, open-source (Apache-2.0) command-line client for the Zentoris
platform. It is a standalone Go module with no dependency on the Zentoris backend source -
it talks to the platform only over HTTP, discovering OAuth/OIDC endpoints at runtime.

## This repository is PUBLIC

- **Never commit secrets.** No tokens, client secrets, API keys, or private URLs - not in
  code, tests, config, or docs. The CLI's own OAuth `client_id` is a public value by design
  (a PKCE public client has no secret); anything that looks like a credential does not belong
  here. Use clearly-fake placeholders in tests (`fake-token`, `test-secret-not-real`).
- **No internal-only details.** No private infrastructure, internal hostnames, account ids,
  or unreleased-product framing. Describe the CLI's own behavior, not Zentoris internals.
- **No competitor-vendor framing** (no "X alternative" / "Y replacement"). Describe shapes
  and standards instead.

## Layout

```
cmd/zentoris/      main package (the entrypoint; `go install .../cmd/zentoris@latest`)
internal/
  api/             HTTP client against the platform API
  auth/            credential sources (token, login, client-credentials, federation),
                   PKCE + loopback, RFC 8628 device flow, keychain/file storage
  commands/        cobra command tree (auth, service, release, version)
  config/          settings resolution (defaults < env < flags)
```

## Conventions

- **Go style is gofmt + go vet, both clean.** No unformatted files, no vet findings.
- **ASCII punctuation only** in code, comments, and config: ` - ` for a dash, `->` for an
  arrow, `...` for an ellipsis. (Markdown docs may be freer, but prefer ASCII for consistency.)
- **cobra commands are noun-first** (`zentoris service list`, gh/kubectl style). One command
  per file under `internal/commands`.
- **Config precedence is defaults < environment < flags.** Every setting has a `ZENTORIS_*`
  environment variable and, where interactive, a flag. Only the `ZENTORIS_` prefix is used -
  no other prefix - to avoid collisions with unrelated environment variables.
- **The OAuth client id and login scope are FIXED constants** in `internal/auth`, not flags
  or env - this is Zentoris's own first-party tooling (like a browser app hardcoding its own
  client id). Only the tenant varies per invocation.
- **Timestamps and randomness:** use the standard library; keep test seams as package vars
  (e.g. a tunable retry interval) rather than reaching for globals in tests.
- **Tests ship with the change.** Table-driven where it fits; a bug fix gets a regression
  test that fails before the fix. Network is faked with an in-process `httptest` server -
  tests never reach a real endpoint.

## Commit and PR style

- **Conventional-commit subjects**, imperative mood: `feat(auth): add device-flow polling`,
  `fix(config): honor ZENTORIS_INSECURE`.
- **No AI/assistant attribution in commit messages or PR bodies** - no "Co-authored-by"
  trailer, no "Generated with" line. Enforced for Claude Code by `.claude/settings.json`
  (`includeCoAuthoredBy: false`); other tools must be configured the same way.
- **Stage only the files you changed** (`git add -- <path>`), never `git add -A`.
- **Commit or push only when asked.**

## Verification before "done"

A change is done when these pass and you have pasted the output:

```bash
gofmt -l .        # prints nothing
go vet ./...
go test ./...
go build ./cmd/zentoris
```
