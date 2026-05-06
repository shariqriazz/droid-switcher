# Account Lifecycle

Last verified: 2026-05-07

## Purpose

This subsystem manages the full lifecycle of a saved Droid account: naming, login capture, current-auth import, active switching, labels, defaults, rename, removal, and backup behavior.

## Key Files

| File | Role |
|------|------|
| `internal/switcher/store.go` | Core account CRUD operations and active/default marker handling |
| `internal/switcher/login.go` | Creates a new saved account by delegating to Droid’s real login flow |
| `internal/switcher/names.go` | Validates explicit account names and generates safe ids for blank input |
| `internal/switcher/metadata.go` | Persists labels and the default-account marker |
| `internal/switcher/fileops.go` | Atomic writes and file copies used during save/switch operations |
| `internal/switcher/constants.go` | Defines the auth files and seeded config files that shape account behavior |
| `internal/switcher/paths.go` | Defines the on-disk layout for saved accounts and active state |

## Flow / How It Works

### Login-backed account creation

1. `internal/switcher/login.go` resolves the account name through `names.go`.
2. If the name is blank, a unique generated id like `account-YYYYMMDD-HHMMSS-xxxxxx` is used.
3. The account gets its own isolated Factory home under `~/.droid-switcher/accounts/<name>/.factory`.
4. Non-auth config files such as `settings.json`, `settings-server.json`, and `mcp.json` are copied into that isolated home.
5. `droid` is launched with `FACTORY_HOME_OVERRIDE=<account-home>`, so Droid itself creates the OAuth state there.
6. After Droid exits, the switcher validates that `auth.v2.file` and `auth.v2.key` exist and marks the account active.

### Saving the currently active local Droid auth

`internal/switcher/store.go::SaveCurrent` imports the auth already present in the real `~/.factory` into a saved account. This path exists for situations where the user has already logged in through Droid before adopting the switcher.

### Switching accounts

`internal/switcher/store.go::SwitchAccount` copies only these files into `~/.factory`:

- `auth.v2.file`
- `auth.v2.key`

If the current real `~/.factory` already contains valid auth, it is first copied into `~/.droid-switcher/backups/<timestamp>/`.

### Metadata and identity

- The stable account id is the directory name under `accounts/`.
- Optional labels are stored in `~/.droid-switcher/accounts/<name>/account.json`.
- The active account marker is separate from the default-account marker.
- The default account marker lives in `~/.droid-switcher/default`.

## Decisions and Trade-offs

- Email addresses are not inferred from auth state or Droid output. The code prefers generated ids plus optional labels over brittle identity scraping.
- `save-current` and `login` share the same account model so imported accounts and Droid-created accounts behave identically later.
- Renaming changes the stable account id and moves the account directory. Labels exist so users do not need to rename accounts just to improve display text.
- Removal clears active/default markers when they reference the deleted account so the CLI does not silently point at missing state.

## Gotchas

- `internal/switcher/store.go` only switches the two auth files. If a future change requires more state to move, the docs and constants must change together.
- `internal/switcher/login.go` seeds some config files into the isolated Factory home before launching Droid. Removing that seeding would make some account homes feel less like the real Droid environment.
- `internal/switcher/metadata.go` stores labels outside the `.factory` directory, so tooling that copies only `.factory` will not preserve friendly labels.
- Force-overwrite behavior for `save-current` is explicit. Without `--force`, existing saved accounts are protected from accidental replacement.

## Related Docs

- [Architecture](./architecture.md)
- [Interactive CLI and Quota](./interactive-cli-and-quota.md)
