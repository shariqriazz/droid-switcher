# Account Lifecycle

Last verified: 2026-08-04

## Purpose

This subsystem manages the full lifecycle of a saved Droid account: naming, login capture, current-auth import, sync-back from live Droid usage, active switching, labels, defaults, rename, removal, and backup behavior.

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
5. `droid` is launched with `FACTORY_HOME_OVERRIDE=<account-root>`, where `<account-root>` is `~/.droid-switcher/accounts/<name>`. Current Droid creates OAuth state in `<account-root>/.factory`.
6. After Droid exits, the switcher validates the resulting auth set. Droid 0.186+ defaults to keyring-v2 (`auth.v2.keyring` with the AES key in the OS keyring); older or keyring-disabled builds produce keyfile-v2 (`auth.v2.file` + `auth.v2.key`). For keyring logins the switcher snapshots the OS keyring key into the account home as `auth.v2.keyring.key` so the account stays portable, and stale files of the other format are pruned.
7. The live `~/.factory` auth is backed up, the newly authenticated auth set is copied into place (restoring the OS keyring key for keyring accounts), and the account is marked active.

Before starting the isolated login, the switcher syncs the previous active
account from live auth. This ordering prevents a later quota or switch command
from copying stale live credentials over a newly authenticated account.

### Saving the currently active local Droid auth

`internal/switcher/store.go::SaveCurrent` imports the auth already present in the real `~/.factory` into a saved account. This path exists for situations where the user has already logged in through Droid before adopting the switcher.

If `--force` is used without a new `--label`, the existing saved label is preserved rather than silently cleared.

### Syncing refreshed live auth back into a saved account

`internal/switcher/store.go::SyncCurrentAuthToSavedAccount` copies the currently live `~/.factory` auth back into the active saved account when:

- the active account marker exists
- both the live and saved auth files are structurally valid
- the saved and live auth contents differ, or the storage formats differ

This exists because normal Droid usage can refresh tokens inside the real `~/.factory`, which would otherwise leave the saved copy stale. When the live home changed storage format (for example a plain Droid re-login landing on keyring-v2), the saved account is converted to the live format, including a fresh keyring key snapshot.

### Switching accounts

`internal/switcher/store.go::SwitchAccount` copies only the files of the account's own storage format into `~/.factory`:

- keyfile-v2: `auth.v2.file` and `auth.v2.key`
- keyring-v2: `auth.v2.keyring`, and the account's key snapshot is written back into the OS keyring so Droid can decrypt it

Files belonging to the other format are moved into the backup directory so Droid never loads a stale login from the wrong backend.

Before the switch, the tool attempts to sync the current live auth back into the active saved account. If the current real `~/.factory` already contains valid auth, it is also copied into `~/.droid-switcher/backups/<timestamp>-<suffix>/` (with a best-effort keyring key snapshot for keyring-format auth).

### Metadata and identity

- The stable account id is the directory name under `accounts/`.
- Optional labels are stored in `~/.droid-switcher/accounts/<name>/account.json`.
- The active account marker is separate from the default-account marker.
- The default account marker lives in `~/.droid-switcher/default`.

## Decisions and Trade-offs

- Email addresses are not inferred from auth state or Droid output. The code prefers generated ids plus optional labels over brittle identity scraping.
- `save-current` and `login` share the same account model so imported accounts and Droid-created accounts behave identically later.
- `login` now follows the same overwrite-safety contract as `save-current`: existing saved accounts require explicit `--force`.
- A successful login is also a real switch: the active marker and live Factory auth always refer to the newly authenticated account.
- Renaming changes the stable account id and moves the account directory. Labels exist so users do not need to rename accounts just to improve display text.
- Removal clears active/default markers when they reference the deleted account so the CLI does not silently point at missing state.

## Gotchas

- `internal/switcher/store.go` only switches the auth files of the account's own format plus the OS keyring key restore. If a future change requires more state to move, the docs and constants must change together.
- Keyring support shells out to libsecret's `secret-tool` because Droid's keytar backend stores the AES key under service `Factory CLI`, account `auth-encryption-key` in the OS secret store. Without `secret-tool`, keyring-format accounts cannot be saved or activated.
- The switcher-owned `auth.v2.keyring.key` snapshot only ever lives in saved account homes and backups, never in the real `~/.factory`.
- `internal/switcher/droid.go` normalizes saved `.factory` paths before setting `FACTORY_HOME_OVERRIDE`; passing the `.factory` directory itself makes current Droid look under `.factory/.factory`.
- `internal/switcher/login.go` seeds some config files into the isolated Factory home before launching Droid. Removing that seeding would make some account homes feel less like the real Droid environment.
- `internal/switcher/metadata.go` stores labels outside the `.factory` directory, so tooling that copies only `.factory` will not preserve friendly labels.
- Force-overwrite behavior for both `save-current` and `login` is explicit. Without `--force`, existing saved accounts are protected from accidental replacement.

## Related Docs

- [Architecture](./architecture.md)
- [Interactive CLI and Quota](./interactive-cli-and-quota.md)
