package switcher

import (
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
)

// authFormat identifies which Droid credential storage backend a Factory home uses.
type authFormat int

const (
	authFormatNone authFormat = iota
	authFormatKeyfile
	authFormatKeyring
)

const (
	droidKeyringService     = "Factory CLI"
	droidKeyringAccountProd = "auth-encryption-key"
)

// readSystemKeyringKey fetches Droid's AES key from the OS secret store. It is
// a variable so tests can stub the store.
var readSystemKeyringKey = func() ([]byte, error) {
	// secret-tool is the libsecret CLI; Droid's keytar backend uses the same
	// service/account attribute pair, so the entries are interchangeable.
	out, err := exec.Command("secret-tool", "lookup", "service", droidKeyringService, "account", droidKeyringAccount()).Output() //nolint:gosec // Fixed arguments; secret-tool has no non-interactive flag set.
	if err != nil {
		return nil, fmt.Errorf("read Droid encryption key from OS keyring: %w (is libsecret's secret-tool installed and the keyring unlocked?)", err)
	}
	key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(out)))
	if err != nil {
		return nil, fmt.Errorf("decode Droid encryption key from OS keyring: %w", err)
	}
	if len(key) != droidAuthKeySize {
		return nil, fmt.Errorf("invalid Droid encryption key length from OS keyring: got %d, want %d", len(key), droidAuthKeySize)
	}
	return key, nil
}

// writeSystemKeyringKey stores Droid's AES key in the OS secret store so the
// live Droid binary can decrypt an activated keyring-format account.
var writeSystemKeyringKey = func(key []byte) error {
	cmd := exec.Command("secret-tool", "store", "--label="+droidKeyringService, "service", droidKeyringService, "account", droidKeyringAccount()) //nolint:gosec // Fixed arguments; the secret arrives over stdin.
	cmd.Stdin = strings.NewReader(base64.StdEncoding.EncodeToString(key))
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("store Droid encryption key in OS keyring: %w: %s", err, compactSpace(string(out)))
	}
	return nil
}

// droidKeyringAccount mirrors Droid's keyring item name, including its dev suffix.
func droidKeyringAccount() string {
	if strings.EqualFold(os.Getenv("FACTORY_ENV"), "development") {
		return droidKeyringAccountProd + "-dev"
	}
	return droidKeyringAccountProd
}

// detectAuthFormat mirrors Droid's credential load order: keyring-v2 wins when
// its file is present, keyfile-v2 is the fallback.
func detectAuthFormat(factoryHome string) authFormat {
	if nonEmptyFile(filepath.Join(factoryHome, authKeyringFileName)) {
		return authFormatKeyring
	}
	if nonEmptyFile(filepath.Join(factoryHome, authFileName)) && nonEmptyFile(filepath.Join(factoryHome, authKeyFileName)) {
		return authFormatKeyfile
	}
	return authFormatNone
}

// requireAuthFormat returns the detected format or an error describing exactly
// which pieces are missing.
func requireAuthFormat(factoryHome string) (authFormat, error) {
	if format := detectAuthFormat(factoryHome); format != authFormatNone {
		return format, nil
	}
	keyringPath := filepath.Join(factoryHome, authKeyringFileName)
	if info, err := os.Stat(keyringPath); err == nil && !info.IsDir() {
		return authFormatNone, fmt.Errorf("%s is empty", authKeyringFileName)
	}
	hasFile, err := fileExists(filepath.Join(factoryHome, authFileName))
	if err != nil {
		return authFormatNone, err
	}
	hasKey, err := fileExists(filepath.Join(factoryHome, authKeyFileName))
	if err != nil {
		return authFormatNone, err
	}
	switch {
	case hasFile && !hasKey:
		return authFormatNone, fmt.Errorf("%s exists but %s is missing", authFileName, authKeyFileName)
	case !hasFile && hasKey:
		return authFormatNone, fmt.Errorf("%s exists but %s is missing", authKeyFileName, authFileName)
	default:
		return authFormatNone, fmt.Errorf("no Droid login found (neither %s nor %s/%s exist)", authKeyringFileName, authFileName, authKeyFileName)
	}
}

// authFilesForFormat lists the Droid-owned files that make up a complete auth
// set for a format. The switcher-owned keyring key snapshot is handled
// separately because it never lives in the real Factory home.
func authFilesForFormat(format authFormat) []string {
	switch format {
	case authFormatKeyring:
		return []string{authKeyringFileName}
	case authFormatKeyfile:
		return []string{authFileName, authKeyFileName}
	default:
		return nil
	}
}

// allDroidAuthFileNames lists every Droid-owned auth file name across formats.
var allDroidAuthFileNames = []string{authFileName, authKeyFileName, authKeyringFileName}

func nonEmptyFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir() && info.Size() > 0
}

// pruneOtherFormatAuth removes auth files that do not belong to the active
// format so Droid never picks up a stale login from the other backend.
func pruneOtherFormatAuth(factoryHome string, keep authFormat) error {
	keepFiles := authFilesForFormat(keep)
	for _, name := range allDroidAuthFileNames {
		if slices.Contains(keepFiles, name) {
			continue
		}
		if err := removeFileIfExists(filepath.Join(factoryHome, name)); err != nil {
			return err
		}
	}
	if keep != authFormatKeyring {
		if err := removeFileIfExists(filepath.Join(factoryHome, authKeyringKeyFileName)); err != nil {
			return err
		}
	}
	return nil
}

func removeFileIfExists(path string) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// readSavedKeyringKey loads the switcher-owned keyring key snapshot from a
// saved account home.
func readSavedKeyringKey(factoryHome string) ([]byte, error) {
	// factoryHome is always a switcher-owned account home, never raw user input.
	raw, err := os.ReadFile(filepath.Join(factoryHome, authKeyringKeyFileName)) //nolint:gosec // Path is scoped by validated account storage.
	if err != nil {
		return nil, err
	}
	key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(raw)))
	if err != nil {
		return nil, fmt.Errorf("decode saved Droid keyring key: %w", err)
	}
	if len(key) != droidAuthKeySize {
		return nil, fmt.Errorf("invalid saved Droid keyring key length: got %d, want %d", len(key), droidAuthKeySize)
	}
	return key, nil
}

// writeSavedKeyringKey stores the keyring key snapshot inside a saved account home.
func writeSavedKeyringKey(factoryHome string, key []byte) error {
	return atomicWriteFile(filepath.Join(factoryHome, authKeyringKeyFileName), []byte(base64.StdEncoding.EncodeToString(key)), 0o600)
}
