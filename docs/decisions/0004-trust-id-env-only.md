# 0004 - `ZENTORIS_TRUST_ID`: environment only, no flag

- Status: Accepted
- Date: 2026-09-23

## Context

The CI credential path (source 4) exchanges the runner's OIDC token for a Zentoris token. The
exchange has to name the trust it runs under: which issuer is accepted, which audience the
runner's token must carry, which claims must match, and which service account the resulting
token acts as. Without that name the request is incomplete and the endpoint rejects it before
verifying anything, so the CLI needs one new input.

Two shapes were on the table: an environment variable only, or an environment variable plus a
`--trust-id` flag.

## Decision

**`ZENTORIS_TRUST_ID`, environment only. No flag.**

1. **It matches the inputs it sits next to.** The other federation inputs - `ZENTORIS_OIDC_TOKEN`
   and `ZENTORIS_OIDC_TOKEN_FILE` - are environment-only too. All three are set once in a job's
   configuration, not typed at a prompt, so a flag would be a second way to say the same thing.

2. **A flag is a public promise** (see [0001](0001-configuration-surface.md)). Adding one later
   if an interactive use case shows up is cheap; removing one is a breaking change. Nothing in
   the current use case - a CI job publishing a service - runs the exchange by hand.

3. **It is an identifier, not a credential.** On its own it authorizes nothing: only a
   validly-signed token whose claims satisfy the trust's own conditions does. So it belongs in
   ordinary CI configuration (a plain variable) rather than a secret store, and the CLI never
   writes it anywhere.

4. **A missing value is a misconfiguration, not an absent credential.** When a CI OIDC token is
   available but `ZENTORIS_TRUST_ID` is unset, the source fails with a message naming the
   variable instead of returning "no credential from this source". Falling through would make
   the chain report "no credential found" for a job that clearly presented an identity, and a
   rejected exchange answers with a deliberately uniform error that cannot explain the cause.

## Consequences

- Federation needs two things in CI: a token the runner mints (with the Zentoris auth base URL
  as its audience) and the trust id. Both are plain configuration.
- The trust's audience and the audience the runner requests have to agree; this is the most
  likely mismatch in a first setup, so the README says so explicitly.
- An interactive `--trust-id` remains available to add later if a non-CI caller ever needs it.
