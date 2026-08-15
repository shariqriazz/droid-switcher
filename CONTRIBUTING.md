# Contributing

## Prerequisites

- Go 1.26.6 or newer
- Git

The lint and vulnerability tools are pinned in `go.mod`; no separate global
tool installation is required.

## Local workflow

```bash
git clone https://github.com/shariqriazz/droid-switcher.git
cd droid-switcher
make verify
```

Use `make fmt` after editing Go files. `make build` creates both command names
under `bin/`, and `make install` installs them into `GOBIN` or `GOPATH/bin`.

Before opening a pull request, run:

```bash
make tidy
make verify
git diff --check
```

Changes to account switching must preserve the sync-back and two-file auth swap
contracts described in [docs/account-lifecycle.md](./docs/account-lifecycle.md).
Changes to quota collection must preserve the raw response fallback described
in [docs/interactive-cli-and-quota.md](./docs/interactive-cli-and-quota.md).
