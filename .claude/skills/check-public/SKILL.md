---
name: check-public
description: Audit the pending change against this repository's public-repo rules before committing. Flags leaked secrets, framing of the Zentoris platform or its API as unreleased/provisional/wireframe, internal hostnames or account ids, competitor-vendor framing, and AI/assistant attribution in the commit message. Use before every commit here, or when asked to "check public", "pre-commit check", or "is this safe to commit".
---

# check-public

`zentoris` is a **PUBLIC**, Apache-2.0 repository (see `AGENTS.md`). Everything committed is
world-readable and permanent. This skill reviews the pending change so nothing that breaks the
public-repo rules gets committed.

Run it before committing. It is **advisory**: the scans below over-match by design, so present
findings for a human to judge. Never auto-commit, and do not edit files as part of the check -
report, then let the user decide.

## Procedure

1. **Scope the change.** Prefer staged content:
   ```bash
   git diff --cached --name-only
   ```
   If nothing is staged, fall back to the working tree and untracked files:
   ```bash
   git diff --name-only; git ls-files --others --exclude-standard
   ```
   Scan only the changed/added content (use `git diff --cached` to see added lines), not the
   whole repo.
2. **Run every scan** in the next section over that content.
3. **Report** grouped by rule: each finding as `path:line`, the matched text, and one line on why
   it is a concern. Finish with a verdict: **PASS** (nothing found) or **REVIEW** (list what to
   eyeball). If the commit message is available, check it too (rule 5).

## Scans

### 1. Secrets (highest severity)
Look for anything credential-shaped in added lines: bearer tokens, `apt_`-prefixed PATs, client
secrets, API keys, `-----BEGIN ... PRIVATE KEY-----` blocks, `Authorization:` headers with
real-looking values, URLs with embedded credentials (`https://user:pass@...`), and long
high-entropy base64/hex strings.

```bash
git diff --cached | grep -nEi 'secret|password|api[_-]?key|bearer [A-Za-z0-9._-]{20,}|apt_[A-Za-z0-9]{16,}|-----BEGIN|://[^/@[:space:]]+:[^/@[:space:]]+@'
```
Allowed and NOT findings: clearly-fake placeholders (`fake-token`, `test-secret-not-real`,
`svc_123`, obvious `example`/`test` values) and the CLI's own OAuth `client_id` value `cli`,
which is a public PKCE client identifier by design, not a secret.

### 2. Platform / API status framing
Flag `wireframe`, `provisional`, `unreleased`, `not shipped`, `coming soon`, or similar when
applied to the **Zentoris platform or its API** - that is internal product status and must not
ship. Honest notes about the **CLI's own** unfinished features (e.g. a `// TODO`, "not yet
wired") are allowed; judge by whether the subject is the platform or the CLI itself.

```bash
git diff --cached | grep -nEi 'wireframe|provisional|unreleased|coming soon|not (yet )?shipped'
```

### 3. Internal infrastructure
Non-public hostnames, internal-looking domains, private IP addresses, account ids, ticket ids, or
personal names in a non-public context. Public hosts derived from `--domain`
(`main.api.<domain>`, `auth.api.<domain>`) and the default `zentoris.com` are fine.

```bash
git diff --cached | grep -nEi '\b(10|192\.168|172\.(1[6-9]|2[0-9]|3[01]))\.[0-9]+|\.internal\b|\.corp\b|\.local\b|localhost:[0-9]+'
```

### 4. Competitor-vendor framing
Flag "X alternative", "Y replacement", or "better than <vendor>" comparisons. Describe shapes and
standards instead.

### 5. Commit-message hygiene
No `Co-authored-by:` trailer and no "Generated with" / assistant-attribution line (also enforced
for Claude Code by `.claude/settings.json` `includeCoAuthoredBy: false`). Subject should be a
conventional-commit line in imperative mood.

## Reporting format

```
check-public: REVIEW
  [secret]   internal/foo.go:42  "api_key = \"sk_live_...\""  -> looks like a real key
  [framing]  README.md:88        "provisional wireframe API"  -> frames the platform as unreleased
PASS items: internal hostnames (none), vendor framing (none), commit message (clean)
```
Report `check-public: PASS` only when every scan is clean.
