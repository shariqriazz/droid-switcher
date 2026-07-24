package switcher

import (
	"fmt"
	"io"
	"os"
	"strings"
)

// Login delegates authentication to Droid, saves the result, and activates it.
func Login(p Paths, name, droidPath, label string, force bool, stdout io.Writer) error {
	name, generated, err := ResolveAccountName(p, name)
	if err != nil {
		return err
	}
	if generated {
		fmt.Fprintf(stdout, "No account name provided. Using generated name: %s\n", name)
	}
	accountHome := p.AccountFactoryHome(name)
	exists, err := fileExists(accountHome)
	if err != nil {
		return err
	}
	existingMeta, err := loadAccountMetadata(p, name)
	if err != nil {
		return err
	}
	if exists && !force {
		return fmt.Errorf("account %q already exists; pass --force to overwrite it", name)
	}
	if err := SyncCurrentAuthToSavedAccount(p, io.Discard); err != nil {
		return fmt.Errorf("sync current auth before login: %w", err)
	}
	if err := os.MkdirAll(accountHome, 0o700); err != nil {
		return err
	}
	if err := SeedNonAuthFactoryFiles(p.FactoryHome, accountHome); err != nil {
		return err
	}

	fmt.Fprintf(stdout, "Starting Droid with FACTORY_HOME_OVERRIDE=%s\n", droidOverrideHome(accountHome))
	fmt.Fprintln(stdout, "Complete Droid login in that session, then exit Droid. This account will be saved automatically.")

	if err := runDroid(NewDroidRunner(droidPath), accountHome); err != nil {
		return err
	}
	if err := EnsureAuth(accountHome); err != nil {
		return fmt.Errorf("login did not create Droid auth files for %q: %w", name, err)
	}
	if strings.TrimSpace(label) == "" && exists {
		label = existingMeta.Label
	}
	if err := saveAccountMetadata(p, name, AccountMetadata{Label: label}); err != nil {
		return err
	}
	return activateSavedAccount(p, name, stdout)
}
