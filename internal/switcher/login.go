package switcher

import (
	"fmt"
	"io"
	"os"
)

func Login(p Paths, name, droidPath, label string, stdout io.Writer) error {
	name, generated, err := ResolveAccountName(p, name)
	if err != nil {
		return err
	}
	if generated {
		fmt.Fprintf(stdout, "No account name provided. Using generated name: %s\n", name)
	}
	accountHome := p.AccountFactoryHome(name)
	if err := os.MkdirAll(accountHome, 0o700); err != nil {
		return err
	}
	if err := SeedNonAuthFactoryFiles(p.FactoryHome, accountHome); err != nil {
		return err
	}

	fmt.Fprintf(stdout, "Starting Droid with FACTORY_HOME_OVERRIDE=%s\n", accountHome)
	fmt.Fprintln(stdout, "Complete Droid login in that session, then exit Droid. This account will be saved automatically.")

	if err := runDroid(NewDroidRunner(droidPath), accountHome); err != nil {
		return err
	}
	if err := EnsureAuth(accountHome); err != nil {
		return fmt.Errorf("login did not create Droid auth files for %q: %w", name, err)
	}
	if err := saveAccountMetadata(p, name, AccountMetadata{Label: label}); err != nil {
		return err
	}
	return writeActive(p, name, stdout)
}
