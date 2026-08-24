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

**Everything committed here is world-readable and permanent - every source file, test,
comment, commit message, and this document itself. There is no private area. Write every
line for a public audience and assume it will be read, indexed, and kept forever. When in
doubt, leave it out.** This ruling is itself public; do not weaken or remove it.

- **Never commit secrets.** No tokens, client secrets, API keys, or private URLs - not in
  code, tests, config, or docs. The CLI's own OAuth `client_id` is a public value by design
  (a PKCE public client has no secret); anything that looks like a credential does not belong
  here. Use clearly-fake placeholders in tests (`fake-token`, `test-secret-not-real`).
- **No internal-only details.** No private infrastructure, internal hostnames, or account ids.
  Do not frame the Zentoris platform or its API as unreleased, provisional, or a "wireframe" -
  that is internal product status and must not appear in code, help text, or docs. Describing
  the CLI's own behavior is fine, including an honest note about which of its own features are
  not yet complete.
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
- **Config precedence is defaults < environment < flags.** Not every setting has an environment
  variable - only the ones worth setting once per shell or injecting in CI do (credentials, the
  account, the endpoint). When a setting has an env var it uses the `ZENTORIS_` prefix and no
  other, to avoid collisions with unrelated environment variables.
- **Keep the flag/env surface small.** A flag or env var is a public promise that is hard to
  remove once shipped; add one only when it earns its place, and prefer deriving a value over
  exposing a new knob. See `docs/decisions/`.
- **The OAuth client id, login scope, and tenant are FIXED constants** in `internal/auth`, not
  flags or env - this is Zentoris's own first-party tooling (like a browser app hardcoding its
  own client id). The single knob that selects a deployment is `--domain` (`ZENTORIS_DOMAIN`),
  from which both the API and auth base URLs derive as `<svc>.api.<domain>`.
- **The CLI emits indented JSON** (`internal/commands.render`); there is no output-format flag
  yet. Add one only when a second format actually exists.
- **Timestamps and randomness:** use the standard library; keep test seams as package vars
  (e.g. a tunable retry interval) rather than reaching for globals in tests.
- **Tests ship with the change.** Table-driven where it fits; a bug fix gets a regression
  test that fails before the fix. Network is faked with an in-process `httptest` server -
  tests never reach a real endpoint.

## Commit and PR style

- **Conventional-commit subjects**, imperative mood: `feat(auth): add device-flow polling`,
  `fix(config): reject a malformed --domain`.
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
