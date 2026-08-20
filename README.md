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
| 3 | client-credentials   | `ZENTORIS_CLIENT_ID` / `ZENTORIS_CLIENT_SECRET` + `--resource` |
| 4 | oidc-federation      | a CI OIDC token (`ZENTORIS_OIDC_TOKEN` / file, or auto-fetched on GitHub Actions) |

### `zentoris auth login`

Interactive sign-in mints a regular platform user session (the same session the web
console gets) and caches it for the active profile.

- **Loopback (default):** opens your browser to a `127.0.0.1` callback. No code to type.
- **Device flow (RFC 8628):** pass `--device`, or let the CLI auto-select it on an SSH
  session or a headless host with no local browser. It prints a code and a URL; you
  approve the request in any browser.

Credentials are stored in your OS keychain (macOS Keychain, Windows Credential Manager,
or the Linux Secret Service) when one is reachable, and in a `0600` file otherwise.
`zentoris auth status` names which store is in use.

## Configuration

Every setting is a flag; the few worth setting once per shell (or injecting in CI) also have an
environment variable. Flags win over environment, which wins over the defaults.

| Setting  | Flag           | Env                | Default                          |
|----------|----------------|--------------------|----------------------------------|
| Main API | `--api`        | -                  | `https://main.api.zentoris.com`  |
| Auth/OP  | `--auth-url`   | -                  | `https://auth.api.zentoris.com`  |
| Tenant   | `--tenant`     | -                  | `main`                           |
| Profile  | `--profile`    | `ZENTORIS_PROFILE` | `default`                        |
| Token    | `--token`      | `ZENTORIS_TOKEN`   | (unset)                          |
| Output   | `-o, --output` | -                  | `table`                          |
| Skip TLS | `--insecure`   | -                  | `false`                          |
| Resource | `--resource`   | -                  | (unset; RFC 8707 audience)       |

`table` output is not implemented yet, so the CLI currently emits JSON regardless of `--output`.

Client-credentials logins read `ZENTORIS_CLIENT_ID` / `ZENTORIS_CLIENT_SECRET` from the
environment (see Authentication).

To reach a self-hosted or otherwise non-default deployment, pin the full URLs:

```bash
zentoris --api https://main.api.your-deployment.example \
         --auth-url https://auth.api.your-deployment.example auth status
```

`--insecure` skips TLS verification and exists only for a self-signed local stack. Never
use it against a real deployment.

## Commands

```
zentoris
  auth
    login [--device]      sign in: loopback (default) or RFC 8628 device flow
    logout                drop stored credentials for the profile
    status                show which credential source is active and where it is stored
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
yet wired), `service`, and `release` are usable. Table output and silent refresh-token
renewal are in progress; today the CLI emits JSON and re-authenticates when a login
expires. Issues and pull requests are welcome.

## License

[Apache-2.0](LICENSE).
