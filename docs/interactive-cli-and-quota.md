# Interactive CLI and Quota

Last verified: 2026-08-04

## Purpose

This subsystem turns the switcher from a raw file-management tool into an operator-friendly CLI: direct commands, interactive menus, number-based account selection, and clean quota views derived from Factory limits for saved Droid accounts.

## Key Files

| File | Role |
|------|------|
| `internal/switcher/cli.go` | Command surface, flags, and textual output for list/current/help |
| `internal/switcher/ui.go` | No-argument menu with guided flows for common tasks |
| `internal/switcher/interactive.go` | Number-based account picker used by `select` and bare `switch` |
| `internal/switcher/quota.go` | Resolves target accounts and renders quota summaries |
| `internal/switcher/auth.go` | Reads and rewrites Droid's encrypted credentials in either storage format (keyfile-v2 or keyring-v2) |
| `internal/switcher/factory_limits.go` | Refreshes expired saved tokens, calls Factory limits, and maps the `5h`, `1wk`, and `1month` windows |
| `internal/switcher/display.go` | Builds user-facing account display strings from labels and ids |
| `internal/switcher/store.go` | Supplies account state that drives menu badges and fallback behavior |
| `internal/switcher/switcher_test.go` | Behavioral tests for menu entry, selection, and quota invocation |

## Flow / How It Works

### Interactive entry points

- Running `droid-switcher` with no arguments enters `internal/switcher/ui.go::RunMenu`.
- The menu offers guided flows for switching, quota, login, save-current, default account, rename, label, and removal operations.
- Menu actions mostly call back into `CLI.Run`, which keeps the interactive layer thin and reuses the same command paths as explicit CLI calls.

### Account selection UX

`internal/switcher/interactive.go::SelectAccount` filters down to ready accounts, then renders a numbered list with badges:

- `*` for active
- `D` for default
- `*D` when both active and default

The user can enter either:

- a number from the menu
- a stable account id by name
- a unique friendly label

If multiple saved accounts share the same label, label-based selection is rejected as ambiguous.

### Quota behavior

`internal/switcher/quota.go::Quota` resolves the target account in this order:

1. Explicit CLI account argument
2. Active account marker
3. Default account marker
4. Interactive picker

Once an account is selected, the switcher:

1. Reads that account's encrypted Droid auth (`auth.v2.file` + `auth.v2.key` for keyfile accounts, or `auth.v2.keyring` + the switcher-owned `auth.v2.keyring.key` snapshot for keyring accounts).
2. Refreshes an expired WorkOS access token with the saved refresh token. Like current Droid, the refresh omits `organization_id`; the stored active organization id is only sent as the `X-Factory-Org-Id` header on the limits call.
3. Calls `GET /api/billing/limits` with Droid-compatible Factory headers.
4. Summarizes every billing group the API reports (`standard`, `core` / Factory Core, and any future groups), each into three windows:

- `5h`
- `1wk`
- `1month`

Windows whose reset time has already passed render as `idle (last window: N% used)` because the API keeps reporting the last consumed window until fresh usage opens a new one. A positive extra-usage balance is rendered as a dollar amount under the groups.

If Factory changes the response format, `--raw` exposes the original JSON API output.

WorkOS `invalid_grant` responses are permanent failures. The CLI identifies
them as ended sessions and prints the account-specific `login --force` recovery
command instead of retrying the same refresh token.

If `quota --all` is used and any account fails quota collection, the command still prints per-account results but returns a non-zero error so scripts can detect partial failure. Using `--all` with an explicit account is treated as invalid input.

## Decisions and Trade-offs

- The menu is text-first and stdlib-driven. That keeps it portable and easy to maintain, even if it is not a full-screen TUI.
- Interactive flows reuse the direct command handlers rather than implementing separate logic paths, which reduces drift between menu and CLI usage.
- Quota rendering is intentionally narrow: it targets the three operator-important windows instead of trying to fully model every possible billing response variant.
- Labels improve UX without replacing stable ids, so users can have friendly names while the storage layer still references deterministic account keys.
- Direct CLI removal requires explicit confirmation unless `--yes` is passed, while the interactive remove flow confirms inline before dispatching the actual command.

## Gotchas

- `internal/switcher/factory_limits.go` depends on Droid's current encrypted auth-file format. Login must continue to be delegated to Droid itself so the switcher does not own OAuth creation.
- `--raw` is the escape hatch if Factory changes the limits JSON shape or adds fields that should be surfaced.
- `internal/switcher/ui.go` currently assumes one line of input per prompt via scanners. If the menu ever becomes persistent or multi-step within one scanner, that input strategy may need to change.
- `internal/switcher/interactive.go` hides accounts without valid auth. That is correct for switching, but it means broken saved accounts disappear from selection until repaired.
- `internal/switcher/cli.go::printCurrent` surfaces the default account when no active marker exists. That is a UX choice, not proof that the default account is currently switched into `~/.factory`.

## Related Docs

- [Architecture](./architecture.md)
- [Account Lifecycle](./account-lifecycle.md)
