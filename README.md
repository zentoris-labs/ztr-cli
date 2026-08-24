# zentoris

The command-line interface for the [Zentoris](https://zentoris.com) platform. Sign in
once, then drive the platform API from your terminal or a CI pipeline.

The binary is named `zentoris`; `ztr` works as a short alias if you set one up.

## Install

### go install

Requires Go 1.26 or newer:

```bash
go install github.com/zentoris-labs/ztr-cli/cmd/zentoris@latest
```

This builds and installs a `zentoris` binary into `$(go env GOPATH)/bin`.

### Prebuilt binaries

Download the archive for your OS and architecture from the
[latest release](https://github.com/zentoris-labs/ztr-cli/releases/latest),
unpack it, and put the `zentoris` binary on your `PATH`.

### Short alias

```bash
alias ztr=zentoris
```

## Quick start

```bash
# sign in (opens a browser; see Authentication for headless machines)
zentoris auth login

# confirm which credential is active and where it is stored
zentoris auth status
```

## Authentication

`zentoris` resolves a bearer credential from a fixed precedence chain. Each method is
one source, so `zentoris auth status` reports which one would be used.

| # | Source               | Input                                          |
|---|----------------------|------------------------------------------------|
| 1 | token                | `--token` / `ZENTORIS_TOKEN` (a personal access token works) |
| 2 | login                | cached by `zentoris auth login`                |
| 3 | client-credentials   | `ZENTORIS_CLIENT_ID` / `ZENTORIS_CLIENT_SECRET` |
| 4 | oidc-federation      | a CI OIDC token (`ZENTORIS_OIDC_TOKEN` / file, or auto-fetched on GitHub Actions) |

### `zentoris auth login`

Interactive sign-in mints a regular platform user session (the same session the web
console gets) and caches it for the active profile.

- **Loopback (default):** opens your browser to a `127.0.0.1` callback. No code to type.
- **Device flow (RFC 8628):** pass `--use-device-code`, or let the CLI auto-select it on an SSH
  session or a headless host with no local browser. It prints a code and a URL; you
  approve the request in any browser.

Credentials are stored in your OS keychain (macOS Keychain, Windows Credential Manager,
or the Linux Secret Service) when one is reachable, and in a `0600` file under `~/.zentoris`
otherwise. `zentoris auth status` names which store is in use.

### Profiles (multiple logins)

A **profile** is a named, separately-stored login - a free-form label you choose (like the AWS
CLI's profiles), not a fixed account id. Sign in under different profile names to keep several
logins side by side, then choose which one runs a command:

```bash
zentoris --profile work auth login                 # sign in; STORES the "work" login (does not switch to it)
zentoris auth switch work                           # make "work" the active profile
zentoris service list                               # runs as the active profile ("work")
zentoris --profile personal auth login --activate   # sign in as "personal" and switch, in one step
zentoris auth list                                  # show all profiles; "*" marks the active one
zentoris --profile work service list                # override for just this command
```

Everything - `auth login`, run commands, `auth logout` - resolves the profile the same way:
**`--profile` > `ZENTORIS_PROFILE` > the active profile**, and the active profile is always set,
starting as `default`. `auth login` is **passive**: it signs into that resolved profile but does
**not** change which one is active - so a bare `auth login` re-logs whichever profile you are
currently on, and `--profile X auth login` stores `X` without switching to it. Activation is a
deliberate step: `auth switch <profile>` (or `auth login --activate` to do both at once).

Each profile's credentials live under their own keychain entry / file, so
`zentoris --profile work auth logout` drops only that profile. `auth status` shows the active
profile, where its credentials are stored, and - when the sign-in token carried one - the account
identity behind it. Profiles apply to `auth login` sessions; a `--token`, client-credentials, or
CI-OIDC credential is a single identity and ignores the profile.

## Configuration

Every setting is a flag; the few worth setting once per shell (or injecting in CI) also have an
environment variable. Flags win over environment, which wins over the defaults.

| Setting  | Flag           | Env                | Default                          |
|----------|----------------|--------------------|----------------------------------|
| Domain   | `--domain`     | `ZENTORIS_DOMAIN`  | `zentoris.com`                   |
| Profile  | `--profile`    | `ZENTORIS_PROFILE` | `default`                        |
| Token    | `--token`      | `ZENTORIS_TOKEN`   | (unset)                          |
| Skip TLS | `--insecure`   | -                  | `false`                          |

The API and auth endpoints are derived from the base domain as `main.api.<domain>` and
`auth.api.<domain>` - `--domain` is the single knob that points the CLI at a deployment,
so there is no separate API and auth URL to set or keep in sync. The OAuth client id, login
scope, and tenant are fixed first-party constants, not settings.

The CLI emits indented JSON.

Client-credentials logins read `ZENTORIS_CLIENT_ID` / `ZENTORIS_CLIENT_SECRET` from the
environment (see Authentication).

To reach a self-hosted or otherwise non-default deployment, point `--domain` at its base
domain (or set `ZENTORIS_DOMAIN`); both endpoints derive from it:

```bash
zentoris --domain your-deployment.example auth status
```

`--insecure` skips TLS verification and exists only for a self-signed local stack. Never
use it against a real deployment.

## Commands

```
zentoris
  auth
    login [--use-device-code] [--activate]   sign in (loopback or device flow); --activate also switches to it
    logout                drop stored credentials for the current profile
    status                show the active profile, credential source, and where it is stored
    list                  list logged-in profiles; "*" marks the active one
    switch <profile>      set the active profile used when --profile is not given
    print-access-token    print the resolved bearer (for scripting)
  service
    list
    get <service-id>
    update <service-id> --set KEY=VALUE ... [--release] [--dry-run]
  release
    create --service <id> [--commit <sha>]
    list   --service <id>
  version
```

Typical CI flow:

```bash
zentoris service update svc_123 --set COMMIT_ID=$GITHUB_SHA --release
```

## Building from source

```bash
git clone https://github.com/zentoris-labs/ztr-cli.git
cd ztr-cli
go build -o zentoris ./cmd/zentoris
./zentoris --help
```

Run the checks the way CI does:

```bash
gofmt -l .
go vet ./...
go test ./...
```

## Status

`auth` (all four sources; OIDC federation acquires the token but its exchange step is not
yet wired), `service`, and `release` are usable. An expired login is renewed silently from its
refresh token before each command, so you sign in once and only re-authenticate when the refresh
token itself expires or is revoked. Issues and pull requests are welcome.

## License

[Apache-2.0](LICENSE).
