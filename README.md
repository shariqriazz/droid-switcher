# droid-switcher

`droid-switcher` is a Go CLI for keeping multiple Factory Droid logins on one
machine, switching the active auth in `~/.factory`, and comparing `/limits`
quota across saved accounts.

It does not reimplement Droid auth. Instead, it launches the real `droid`
binary with `FACTORY_HOME_OVERRIDE` so each saved account goes through Droid's
normal OAuth flow.

## What It Does

- Stores any number of Droid accounts locally
- Switches the active `~/.factory` auth between saved accounts
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
droid-switcher quota --all
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
- add a new Droid login
- save the current `~/.factory` auth
- set default account
- rename an account
- manage labels
- remove an account

`droid-switcher select` and bare `droid-switcher switch` also support picking
accounts by number.

## Common Commands

### Add accounts

```bash
droid-switcher login work
droid-switcher login work --label "Main Work"
droid-switcher login personal
droid-switcher login
```

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
```

Use this when you already logged in through normal Droid before using the
switcher.

### Switch accounts

```bash
droid-switcher switch work
droid-switcher switch
droid-switcher select
```

Only these files are swapped into the real `~/.factory`:

- `auth.v2.file`
- `auth.v2.key`

Current auth is backed up before replacement when valid auth already exists.

### Quota

```bash
droid-switcher quota work
droid-switcher quota
droid-switcher quota --all
droid-switcher quota --all --raw
```

Quota uses Droid's `/limits` output and summarizes:

- `5h`
- `1wk`
- `1month`

Use `--raw` if Factory changes the `/limits` display and you want to inspect
the original Droid output.

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
droid-switcher remove old-name
droid-switcher where
droid-switcher help
```

## Local Files

- Active Droid auth: `~/.factory/auth.v2.file` and `~/.factory/auth.v2.key`
- Saved accounts: `~/.droid-switcher/accounts/<name>/.factory`
- Account labels: `~/.droid-switcher/accounts/<name>/account.json`
- Default account: `~/.droid-switcher/default`
- Switch backups: `~/.droid-switcher/backups`

## Documentation

- [Documentation Index](./docs/INDEX.md)
- [Architecture](./docs/architecture.md)
- [Account Lifecycle](./docs/account-lifecycle.md)
- [Interactive CLI and Quota](./docs/interactive-cli-and-quota.md)

## License

[MIT](./LICENSE)
