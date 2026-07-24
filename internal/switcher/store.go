package switcher

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// AccountStatus combines saved auth readiness with active/default metadata.
type AccountStatus struct {
	Name    string
	Label   string
	Active  bool
	Default bool
	Ready   bool
}

// SaveOptions controls replacement and labeling when importing live auth.
type SaveOptions struct {
	Force bool
	Label string
}

// SaveCurrent imports the live Factory auth pair into a saved account.
func SaveCurrent(p Paths, name string, opts SaveOptions, stdout io.Writer) error {
	name, generated, err := ResolveAccountName(p, name)
	if err != nil {
		return err
	}
	if generated {
		fmt.Fprintf(stdout, "No account name provided. Using generated name: %s\n", name)
	}
	if err := EnsureAuth(p.FactoryHome); err != nil {
		return fmt.Errorf("current Factory home is not logged in: %w", err)
	}
	dst := p.AccountFactoryHome(name)
	exists, err := fileExists(dst)
	if err != nil {
		return err
	}
	existingMeta, err := loadAccountMetadata(p, name)
	if err != nil {
		return err
	}
	if !opts.Force {
		if exists {
			return fmt.Errorf("account %q already exists; pass --force to overwrite it", name)
		}
	}
	if err := os.MkdirAll(dst, 0o700); err != nil {
		return err
	}
	if err := copyDirFiles(p.FactoryHome, dst, 0o600, authFiles); err != nil {
		return err
	}
	label := strings.TrimSpace(opts.Label)
	if label == "" && exists {
		label = existingMeta.Label
	}
	if err := saveAccountMetadata(p, name, AccountMetadata{Label: label}); err != nil {
		return err
	}
	return writeActive(p, name, stdout)
}

// SwitchAccount syncs the previous account and activates a saved auth pair.
func SwitchAccount(p Paths, name string, stdout io.Writer) error {
	name, err := CleanAccountName(name)
	if err != nil {
		return err
	}
	src := p.AccountFactoryHome(name)
	if err := EnsureAuth(src); err != nil {
		return fmt.Errorf("saved account %q is not usable: %w", name, err)
	}
	if err := SyncCurrentAuthToSavedAccount(p, io.Discard); err != nil {
		return err
	}
	return activateSavedAccount(p, name, stdout)
}

func activateSavedAccount(p Paths, name string, stdout io.Writer) error {
	src := p.AccountFactoryHome(name)
	if err := os.MkdirAll(p.FactoryHome, 0o700); err != nil {
		return err
	}
	if err := BackupCurrentAuth(p); err != nil {
		return err
	}
	for _, file := range authFiles {
		if err := atomicCopy(filepath.Join(src, file), filepath.Join(p.FactoryHome, file), 0o600); err != nil {
			return err
		}
	}
	return writeActive(p, name, stdout)
}

// ListAccounts returns saved accounts sorted by stable id.
func ListAccounts(p Paths) ([]AccountStatus, error) {
	entries, err := os.ReadDir(p.Accounts)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	active, _ := os.ReadFile(p.ActiveFile)
	activeName := strings.TrimSpace(string(active))
	defaultName, _, _ := CurrentDefaultAccount(p)

	var accounts []AccountStatus
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		meta, err := loadAccountMetadata(p, name)
		if err != nil {
			return nil, err
		}
		accounts = append(accounts, AccountStatus{
			Name:    name,
			Label:   meta.Label,
			Active:  name == activeName,
			Default: name == defaultName,
			Ready:   EnsureAuth(p.AccountFactoryHome(name)) == nil,
		})
	}
	sort.Slice(accounts, func(i, j int) bool {
		return accounts[i].Name < accounts[j].Name
	})
	return accounts, nil
}

// CurrentAccount returns the account whose auth is active in the live home.
func CurrentAccount(p Paths) (string, bool, error) {
	active, err := os.ReadFile(p.ActiveFile)
	if errors.Is(err, os.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	name := strings.TrimSpace(string(active))
	return name, name != "", nil
}

// RemoveAccount deletes a saved account and clears markers that reference it.
func RemoveAccount(p Paths, name string, stdout io.Writer) error {
	name, err := CleanAccountName(name)
	if err != nil {
		return err
	}
	root := filepath.Join(p.Accounts, name)
	if _, err := os.Stat(root); err != nil {
		return fmt.Errorf("account %q does not exist: %w", name, err)
	}
	if err := os.RemoveAll(root); err != nil {
		return err
	}
	active, _, _ := CurrentAccount(p)
	if active == name {
		_ = os.Remove(p.ActiveFile)
	}
	defaultName, ok, _ := CurrentDefaultAccount(p)
	if ok && defaultName == name {
		_ = clearDefaultAccount(p)
	}
	fmt.Fprintf(stdout, "Removed %s\n", name)
	return nil
}

// RenameAccount changes a stable id and updates active/default markers.
func RenameAccount(p Paths, oldName, newName string, stdout io.Writer) error {
	oldName, err := CleanAccountName(oldName)
	if err != nil {
		return err
	}
	newName, err = CleanAccountName(newName)
	if err != nil {
		return err
	}
	oldPath := filepath.Join(p.Accounts, oldName)
	newPath := filepath.Join(p.Accounts, newName)
	if _, err := os.Stat(oldPath); err != nil {
		return fmt.Errorf("account %q does not exist: %w", oldName, err)
	}
	if _, err := os.Stat(newPath); err == nil {
		return fmt.Errorf("account %q already exists", newName)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Rename(oldPath, newPath); err != nil {
		return err
	}
	active, _, _ := CurrentAccount(p)
	if active == oldName {
		if err := writeActive(p, newName, io.Discard); err != nil {
			return err
		}
	}
	defaultName, ok, _ := CurrentDefaultAccount(p)
	if ok && defaultName == oldName {
		if err := writeDefaultAccount(p, newName); err != nil {
			return err
		}
	}
	fmt.Fprintf(stdout, "Renamed %s to %s\n", oldName, newName)
	return nil
}

// SetAccountLabel updates optional display metadata without changing the id.
func SetAccountLabel(p Paths, name, label string, stdout io.Writer) error {
	name, err := CleanAccountName(name)
	if err != nil {
		return err
	}
	if _, err := os.Stat(accountRoot(p, name)); err != nil {
		return fmt.Errorf("account %q does not exist: %w", name, err)
	}
	if err := saveAccountMetadata(p, name, AccountMetadata{Label: label}); err != nil {
		return err
	}
	if strings.TrimSpace(label) == "" {
		fmt.Fprintf(stdout, "Cleared label for %s\n", name)
		return nil
	}
	fmt.Fprintf(stdout, "Label for %s: %s\n", name, strings.TrimSpace(label))
	return nil
}

// SetDefaultAccount configures the fallback for quota and menu flows.
func SetDefaultAccount(p Paths, name string, stdout io.Writer) error {
	name, err := CleanAccountName(name)
	if err != nil {
		return err
	}
	if _, err := os.Stat(accountRoot(p, name)); err != nil {
		return fmt.Errorf("account %q does not exist: %w", name, err)
	}
	if err := writeDefaultAccount(p, name); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "Default account: %s\n", name)
	return nil
}

// ClearDefaultAccount removes the optional fallback marker.
func ClearDefaultAccount(p Paths, stdout io.Writer) error {
	if err := clearDefaultAccount(p); err != nil {
		return err
	}
	fmt.Fprintln(stdout, "Cleared default account")
	return nil
}

// EnsureAuth verifies that both required Droid auth files are non-empty.
func EnsureAuth(factoryHome string) error {
	for _, file := range authFiles {
		info, err := os.Stat(filepath.Join(factoryHome, file))
		if err != nil {
			return err
		}
		if info.IsDir() || info.Size() == 0 {
			return fmt.Errorf("%s is empty or not a file", file)
		}
	}
	return nil
}

// SeedNonAuthFactoryFiles copies safe configuration into an isolated login home.
func SeedNonAuthFactoryFiles(srcFactoryHome, dstFactoryHome string) error {
	for _, file := range seedFiles {
		src := filepath.Join(srcFactoryHome, file)
		if _, err := os.Stat(src); err == nil {
			if err := copyFile(src, filepath.Join(dstFactoryHome, file), 0o600); err != nil {
				return err
			}
		}
	}
	return nil
}

// BackupCurrentAuth snapshots valid live auth before it is replaced.
func BackupCurrentAuth(p Paths) error {
	if EnsureAuth(p.FactoryHome) != nil {
		return nil
	}
	backupDir := filepath.Join(p.Store, "backups", fmt.Sprintf("%s-%s", time.Now().Format("20060102-150405"), randomSuffix()[:6]))
	if err := os.MkdirAll(backupDir, 0o700); err != nil {
		return err
	}
	return copyDirFiles(p.FactoryHome, backupDir, 0o600, authFiles)
}

func writeActive(p Paths, name string, stdout io.Writer) error {
	if err := os.MkdirAll(p.Store, 0o700); err != nil {
		return err
	}
	if err := atomicWriteFile(p.ActiveFile, []byte(name+"\n"), 0o600); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "Active account: %s\n", name)
	return nil
}

// SyncCurrentAuthToSavedAccount preserves live token refreshes for the active account.
func SyncCurrentAuthToSavedAccount(p Paths, stdout io.Writer) error {
	active, ok, err := CurrentAccount(p)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	src := p.FactoryHome
	dst := p.AccountFactoryHome(active)
	if err := EnsureAuth(src); err != nil {
		return nil
	}
	if err := EnsureAuth(dst); err != nil {
		return nil
	}
	changed := false
	for _, file := range authFiles {
		same, err := sameContent(filepath.Join(src, file), filepath.Join(dst, file))
		if err != nil {
			return err
		}
		if !same {
			changed = true
			break
		}
	}
	if !changed {
		return nil
	}
	if err := copyDirFiles(src, dst, 0o600, authFiles); err != nil {
		return err
	}
	if stdout != nil {
		fmt.Fprintf(stdout, "Synced current auth back to saved account: %s\n", active)
	}
	return nil
}
