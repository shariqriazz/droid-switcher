# Interactive CLI and Quota

Last verified: 2026-05-07

## Purpose

This subsystem turns the switcher from a raw file-management tool into an operator-friendly CLI: direct commands, interactive menus, number-based account selection, and clean quota views derived from Droid `/limits`.

## Key Files

| File | Role |
|------|------|
| `internal/switcher/cli.go` | Command surface, flags, and textual output for list/current/help |
| `internal/switcher/ui.go` | No-argument menu with guided flows for common tasks |
| `internal/switcher/interactive.go` | Number-based account picker used by `select` and bare `switch` |
| `internal/switcher/quota.go` | Runs `/limits`, parses the `5h`, `1wk`, and `1month` windows, and renders summaries |
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

Once an account is selected, the switcher runs:

`droid exec --output-format text /limits`

under that account’s isolated Factory home. It then attempts to summarize three windows:

- `5h`
- `1wk`
- `1month`

If parsing fails or Factory changes the output format, `--raw` exposes the original Droid output.

If `quota --all` is used and any account fails quota collection, the command still prints per-account results but returns a non-zero error so scripts can detect partial failure. Using `--all` with an explicit account is treated as invalid input.

## Decisions and Trade-offs

- The menu is text-first and stdlib-driven. That keeps it portable and easy to maintain, even if it is not a full-screen TUI.
- Interactive flows reuse the direct command handlers rather than implementing separate logic paths, which reduces drift between menu and CLI usage.
- Quota parsing is intentionally narrow: it targets the three operator-important windows instead of trying to fully model every possible `/limits` output variant.
- Labels improve UX without replacing stable ids, so users can have friendly names while the storage layer still references deterministic account keys.
- Direct CLI removal requires explicit confirmation unless `--yes` is passed, while the interactive remove flow confirms inline before dispatching the actual command.

## Gotchas

- `internal/switcher/quota.go` parses human-readable text, not a stable API response. Keep `--raw` working as the escape hatch.
- `internal/switcher/ui.go` currently assumes one line of input per prompt via scanners. If the menu ever becomes persistent or multi-step within one scanner, that input strategy may need to change.
- `internal/switcher/interactive.go` hides accounts without valid auth. That is correct for switching, but it means broken saved accounts disappear from selection until repaired.
- `internal/switcher/cli.go::printCurrent` surfaces the default account when no active marker exists. That is a UX choice, not proof that the default account is currently switched into `~/.factory`.

## Related Docs

- [Architecture](./architecture.md)
- [Account Lifecycle](./account-lifecycle.md)
