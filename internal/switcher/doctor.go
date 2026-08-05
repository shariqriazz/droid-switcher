package switcher

import (
	"bytes"
	"fmt"
	"io"
)

// DoctorOptions controls keyring diagnosis and optional repair.
type DoctorOptions struct {
	Heal bool
}

// Doctor inspects Droid's OS keyring against the live and saved homes. With
// Heal set it rewrites the keyring to a single entry holding the key that
// decrypts the live home, which is the state Droid needs at startup. Secret
// backends that keep duplicate entries (ksecretd) and Droid's own fresh-key
// generation after keyring read failures can otherwise leave lookups answering
// with a stale key, which surfaces as an unexpected login prompt.
func Doctor(p Paths, opts DoctorOptions, stdout io.Writer) error {
	format := detectAuthFormat(p.FactoryHome)
	fmt.Fprintf(stdout, "Live Factory home auth format: %s\n", authFormatName(format))
	switch format {
	case authFormatNone:
		fmt.Fprintln(stdout, "No Droid login in the live home; run droid-switcher login or switch to a saved account.")
		return nil
	case authFormatKeyfile:
		fmt.Fprintln(stdout, "Keyfile format does not use the OS keyring; nothing to diagnose.")
		return nil
	}

	lookup, lookupErr := readSystemKeyringKey()
	entries, listErr := readAllSystemKeyringKeys()
	switch {
	case listErr == nil:
		fmt.Fprintf(stdout, "OS keyring entries for Droid's key: %d\n", len(entries))
	case lookupErr == nil:
		entries = [][]byte{lookup}
		fmt.Fprintln(stdout, "OS keyring entries for Droid's key: 1 (plain lookup only; listing entries failed)")
	default:
		return fmt.Errorf("read Droid encryption key from OS keyring: %w (is the keyring unlocked?)", listErr)
	}

	var correct []byte
	decrypting := 0
	for _, entry := range entries {
		if keyringKeyDecryptsHome(p.FactoryHome, entry) {
			decrypting++
			if correct == nil {
				correct = entry
			}
		}
	}
	lookupOK := lookupErr == nil && keyringKeyDecryptsHome(p.FactoryHome, lookup)
	fmt.Fprintf(stdout, "Plain keyring lookup decrypts the live home: %s\n", yesNo(lookupOK))
	if len(entries) > 1 {
		fmt.Fprintf(stdout, "Duplicate keyring entries hold %d distinct keys; %d of %d decrypt the live home.\n", len(entries), decrypting, len(entries))
	}
	if correct == nil {
		fmt.Fprintln(stdout, "No OS keyring entry decrypts the live home; Droid will ask for login.")
		printDoctorRecovery(p, stdout)
		return fmt.Errorf("no OS keyring entry decrypts %s", authKeyringFileName)
	}

	printDoctorSavedAccounts(p, stdout)

	healthy := len(entries) == 1 && lookupOK
	if healthy {
		fmt.Fprintln(stdout, "Keyring state is healthy.")
		return nil
	}
	if !opts.Heal {
		fmt.Fprintln(stdout, "Run droid-switcher doctor --heal to rewrite the keyring to the single working key.")
		return nil
	}

	fmt.Fprintln(stdout, "Healing: rewriting the OS keyring to the single key that decrypts the live home...")
	if err := writeSystemKeyringKey(correct); err != nil {
		return err
	}
	after, err := readAllSystemKeyringKeys()
	if err != nil {
		fmt.Fprintf(stdout, "Healed the lookup result, but re-listing entries failed: %v\n", err)
		return nil
	}
	strays := 0
	for _, entry := range after {
		if !bytes.Equal(entry, correct) {
			strays++
		}
	}
	if strays > 0 {
		fmt.Fprintf(stdout, "The secret-service backend kept %d stale entries holding other keys; Droid may still read one of them. "+
			"Delete the remaining %q/%q items manually (KDE Wallet Manager, or busctl --user call org.freedesktop.secrets <item-path> org.freedesktop.Secret.Item Delete).\n",
			strays, droidKeyringService, droidKeyringAccount())
		return fmt.Errorf("secret-service backend kept %d stale keyring entries after heal", strays)
	}
	fmt.Fprintln(stdout, "Keyring healed: exactly one entry remains and it decrypts the live home.")
	return nil
}

// printDoctorRecovery points at the cheapest working recovery when the OS
// keyring lost the live home's key entirely.
func printDoctorRecovery(p Paths, stdout io.Writer) {
	active, ok, _ := CurrentAccount(p)
	if ok && EnsureSavedAuth(p.AccountFactoryHome(active)) == nil {
		fmt.Fprintf(stdout, "Recovery: run droid-switcher switch %s to restore the live home from the verified saved copy.\n", active)
		return
	}
	fmt.Fprintln(stdout, "Recovery: run droid-switcher switch <account> for any account listed as ready below, or droid-switcher login --force to re-login.")
	printDoctorSavedAccounts(p, stdout)
}

// printDoctorSavedAccounts lists saved accounts with their usability status.
func printDoctorSavedAccounts(p Paths, stdout io.Writer) {
	accounts, err := ListAccounts(p)
	if err != nil || len(accounts) == 0 {
		return
	}
	fmt.Fprintln(stdout, "Saved accounts:")
	for _, account := range accounts {
		state := "ready"
		if err := EnsureSavedAuth(p.AccountFactoryHome(account.Name)); err != nil {
			state = "unusable: " + err.Error()
		}
		marker := " "
		if account.Active {
			marker = "*"
		}
		fmt.Fprintf(stdout, "  %s %-36s %s\n", marker, displayAccount(account), state)
	}
}

func authFormatName(format authFormat) string {
	switch format {
	case authFormatKeyfile:
		return "keyfile-v2 (auth.v2.file/auth.v2.key)"
	case authFormatKeyring:
		return "keyring-v2 (auth.v2.keyring)"
	default:
		return "none"
	}
}

func yesNo(ok bool) string {
	if ok {
		return "yes"
	}
	return "no"
}
