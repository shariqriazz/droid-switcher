# droid-switcher

`droid-switcher` is a Go CLI for keeping multiple Factory Droid logins on one
machine, switching the active auth in `~/.factory`, and comparing `/limits`
quota across saved accounts.

It does not reimplement Droid auth. Instead, it launches the real `droid`
binary with `FACTORY_HOME_OVERRIDE` so each saved account goes through Droid's
normal OAuth flow.

## Installation

Build and install both `droid-switcher` and its `drsw` shorthand:

```bash
make install
```

The Makefile installs into `GOBIN`, or the first `GOPATH/bin` directory when
`GOBIN` is unset.

## What It Does

- Stores any number of Droid accounts locally
- Switches the active `~/.factory` auth between saved accounts
- Syncs refreshed live auth back into the saved active account
- Keeps friendly labels separate from stable account ids
- Supports an active account and an optional default account
- Shows clean quota summaries for the `5h`, `1wk`, and `1month` `/limits` windows
- Provides an interactive numbered menu when run with no arguments

## Quick Start

Add a couple of accounts:

```bash
droid-switcher login work --label "Main Work"
droid-switcher login personal --label "Personal"
```

Open the interactive menu:

```bash
droid-switcher
```

Compare quota across all saved accounts:

```bash
drsw q -a
```

Set the default fallback account:

```bash
droid-switcher default work
```

## How Account Selection Works

For commands that need an account, the selection order is:

1. Explicit account argument
2. Active account
3. Default account
4. Interactive picker

When the picker is used, you can select by:

- number
- stable account id
- unique friendly label

That matters most for:

- `droid-switcher quota`
- `droid-switcher switch`
- the interactive menu flows

## Interactive Mode

Running `droid-switcher` with no arguments opens a numbered menu for the most
common operations:

- switch account
- show quota
- compare quota across all accounts
- share sessions across accounts
- add a new Droid login
- save the current `~/.factory` auth
- set default account
- rename an account
- manage labels
- remove an account

`droid-switcher select` and bare `droid-switcher switch` also support picking
accounts by number. Destructive removal is confirmed in the interactive flow.

## Common Commands

### Add accounts

```bash
droid-switcher login work
droid-switcher login work --label "Main Work"
droid-switcher login work --force
droid-switcher login personal
droid-switcher login
```

After login completes, the new saved auth is copied into `~/.factory` and the
account becomes active. Existing live auth is backed up first.

If you leave the account name blank, the switcher generates a safe id like:

```text
account-20260507-041530-a1b2c3
```

Generated ids are meant to be stable storage keys. Use labels to make them nice
to look at.

### Save the current local Droid auth

```bash
droid-switcher save-current work
droid-switcher save-current --label "Imported Work"
droid-switcher save-current
droid-switcher save-current work --force
droid-switcher sync-current
```

Use this when you already logged in through normal Droid before using the
switcher. `sync-current` is the manual escape hatch for copying the currently
live `~/.factory` auth back into the active saved account after normal Droid use
refreshes tokens.

### Switch accounts

```bash
droid-switcher switch work
droid-switcher switch
droid-switcher select
```

All current Droid credential storage formats on macOS and Linux are supported,
and only the files of the
account's own format are swapped into the real `~/.factory`:

- keyfile-v2: `auth.v2.file` + `auth.v2.key`
- keyring-v2 (Linux): `auth.v2.keyring`, plus a switcher-owned
  snapshot of the OS keyring encryption key (`auth.v2.keyring.key`, stored only
  in the saved account home)
- login-keychain-v2 (macOS, including Droid 0.193+):
  `auth.v2.loginkeychain`, plus a switcher-owned key snapshot
  (`auth.v2.loginkeychain.key`, stored only in the saved account home)

Switching to a secure-storage account writes that account's encryption key back
into the native OS store. Linux uses service `Factory CLI`, account
`auth-encryption-key`, via libsecret's `secret-tool`. macOS uses service
`Factory CLI`, account `auth-encryption-key-security-cli`, via the built-in
`/usr/bin/security` tool. Files of the other formats are moved into the backup
directory so Droid cannot pick up a stale login. Linux secure-storage support
therefore requires `secret-tool` (package `libsecret`); macOS has no additional
dependency.

Key snapshots are always verified against the encrypted credentials before
being saved: if the keyring holds stale duplicate entries (some backends keep
several, and Droid adds one when it generates a fresh key after a keyring read
failure), every entry is tried and the one that actually decrypts wins.
Activation rewrites the keyring to a single verified entry, so lookups can
never return a key that does not match the active account.

If Droid still asks for login after a switch, the keyring was most likely
disturbed after activation: Droid generates and stores a fresh key whenever its
own keyring read fails (a locked or flaky secret-service backend, e.g.
ksecretd), leaving a duplicate entry that lookups may answer instead of the
correct one. Diagnose and repair without re-logging in:

```bash
droid-switcher doctor          # report keyring entries vs. the live auth
droid-switcher doctor --heal   # rewrite the keyring to the single working key
```

`doctor --heal` keeps the key that actually decrypts the live `~/.factory` and
removes every other entry, so it can also recover the state after Droid
generated a fresh key. It refuses to touch the keyring when no entry decrypts
the live auth and prints the recovery path instead (usually
`droid-switcher switch <account>` to restore from the verified saved copy).

Current auth is backed up before replacement when valid auth already exists.
Before switching away from an active saved account, the switcher also syncs the
live auth back into that saved account when it detects changes.

### Session Sharing Across Accounts

Droid permanently stamps each session with the active account's organization ID.
When you switch to an account with a different organization ID, Droid hides
sessions created under other organizations from `droid resume` and rejects
resuming them.

`droid-switcher` automatically unlocks sessions across accounts by default
whenever you switch or log in, so you never lose your sessions when switching
accounts:

```bash
# Switching accounts unlocks sessions automatically:
drsw switch work

# To keep organizations isolated instead, pass --no-share:
drsw switch work --no-share

# Share all sessions manually at any time:
drsw --share
drsw share
drsw -s

# Preview changes without modifying files:
drsw share --dry-run

# List sessions and their current organization binding:
drsw share --list
```

### Quota

```bash
drsw q work
drsw q
drsw q -a
drsw q -a -r
```

Quota uses Factory's limits API with each saved Droid login and summarizes the
same operator-facing windows as `/limits` for every billing group the API
reports (`standard`, `core` / Factory Core, and any future groups):

- `5h`
- `1wk`
- `1month`

Windows whose reset time has already passed are shown as
`idle (last window: N% used)` instead of presenting the expired window's usage
as current; the API keeps reporting the last consumed window until fresh usage
opens a new one. A positive extra-usage balance is shown as a dollar amount.

Use `--raw`/`-r` if Factory changes the limits response and you want to inspect
the original API output.

If WorkOS reports that a saved session has ended, authenticate that account
again:

```bash
droid-switcher login work --force
```

### Labels and defaults

```bash
droid-switcher label work "Main Work Org"
droid-switcher label work --clear
droid-switcher default work
droid-switcher default --clear
```

Labels improve list, selection, current-account, and quota output without
changing the underlying saved account id.

### Other account management

```bash
droid-switcher list
droid-switcher current
droid-switcher rename old-name new-name
droid-switcher sync-current
droid-switcher doctor
droid-switcher doctor --heal
droid-switcher remove old-name
droid-switcher remove old-name --yes
droid-switcher where
droid-switcher help
droid-switcher version
```

## Development

The project targets Go 1.26.6 and pins its lint and vulnerability tools through
`go.mod`.

```bash
make test        # unit tests
make test-race   # shuffled tests with race detection
make lint        # golangci-lint, staticcheck, gosec, vet-style checks
make vuln        # govulncheck
make verify      # complete local quality gate
make build       # creates bin/droid-switcher and bin/drsw
```

CI runs the same test, static-analysis, and vulnerability checks. Dependabot
keeps Go tooling and GitHub Actions updates visible as reviewable pull requests.

## Local Files

- Active Droid auth: `***********************` and `~/.factory/auth.v2.key` (keyfile), `~/.factory/auth.v2.keyring` (Linux keyring), or `~/.factory/auth.v2.loginkeychain` (macOS Login Keychain)
- Saved accounts: `~/.droid-switcher/accounts/<name>/.factory`
- Account labels: `~/.droid-switcher/accounts/<name>/account.json`
- Default account: `~/.droid-switcher/default`
- Switch backups: `~/.droid-switcher/backups`

## Documentation

- [Documentation Index](./docs/INDEX.md)
- [Architecture](./docs/architecture.md)
- [Account Lifecycle](./docs/account-lifecycle.md)
- [Interactive CLI and Quota](./docs/interactive-cli-and-quota.md)
- [Contributing](./CONTRIBUTING.md)

## License

[MIT](./LICENSE)
