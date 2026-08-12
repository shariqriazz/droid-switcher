package switcher

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
)

// authFormat identifies which Droid credential storage backend a Factory home uses.
type authFormat int

const (
	authFormatNone authFormat = iota
	authFormatKeyfile
	authFormatKeyring
	authFormatLoginKeychain
)

const (
	droidKeyringService                = "Factory CLI"
	droidKeyringAccountProd            = "auth-encryption-key"
	droidLoginKeychainAccountProd      = "auth-encryption-key-security-cli"
	macOSSecurityCLI                   = "/usr/bin/security"
	macOSSecurityCLIItemNotFoundStatus = 44
)

// readSystemKeyringKey fetches Droid's AES key from the OS secret store. It is
// a variable so tests can stub the store.

var readSystemKeyringKey = func(format authFormat) ([]byte, error) {
	account, err := droidSecureStorageAccount(format)
	if err != nil {
		return nil, err
	}
	if runtime.GOOS == "darwin" {
		out, err := exec.Command(macOSSecurityCLI, "find-generic-password", "-s", droidKeyringService, "-a", account, "-w").Output() //nolint:gosec // Fixed executable and arguments; output stays in process memory.
		if err != nil {
			return nil, fmt.Errorf("read Droid encryption key from macOS Login Keychain: %w (is the login keychain unlocked?)", err)
		}
		return decodeSystemKey(out, "macOS Login Keychain")
	}
	if runtime.GOOS != "linux" {
		return nil, fmt.Errorf("droid secure credential storage is unsupported on %s", runtime.GOOS)
	}
	if format != authFormatKeyring {
		return nil, fmt.Errorf("%s is only supported on macOS", authLoginKeychainFileName)
	}
	// secret-tool is the libsecret CLI; Droid's keytar backend uses the same
	// service/account attribute pair, so the entries are interchangeable.
	out, err := exec.Command("secret-tool", "lookup", "service", droidKeyringService, "account", account).Output() //nolint:gosec // Fixed arguments; secret-tool has no non-interactive flag set.
	if err != nil {
		return nil, fmt.Errorf("read Droid encryption key from OS keyring: %w (is libsecret's secret-tool installed and the keyring unlocked?)", err)
	}
	return decodeSystemKey(out, "OS keyring")
}

func decodeSystemKey(raw []byte, source string) ([]byte, error) {
	key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(raw)))
	if err != nil {
		return nil, fmt.Errorf("decode Droid encryption key from %s: %w", source, err)
	}
	if len(key) != droidAuthKeySize {
		return nil, fmt.Errorf("invalid Droid encryption key length from %s: got %d, want %d", source, len(key), droidAuthKeySize)
	}
	return key, nil
}

// storeSystemKeyringKey writes Droid's AES key to the OS secret store without
// touching any other entries. Use writeSystemKeyringKey instead; it guarantees
// the single-entry invariant.
var storeSystemKeyringKey = func(format authFormat, key []byte) error {
	account, err := droidSecureStorageAccount(format)
	if err != nil {
		return err
	}
	encoded := base64.StdEncoding.EncodeToString(key)
	if runtime.GOOS == "darwin" {
		command, err := macOSKeychainAddCommand(droidKeyringService, account, encoded)
		if err != nil {
			return err
		}
		cmd := exec.Command(macOSSecurityCLI, "-i")
		cmd.Stdin = strings.NewReader(command + "\n")
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("store Droid encryption key in macOS Login Keychain: %w: %s", err, compactSpace(string(out)))
		}
		return nil
	}
	if runtime.GOOS != "linux" {
		return fmt.Errorf("droid secure credential storage is unsupported on %s", runtime.GOOS)
	}
	if format != authFormatKeyring {
		return fmt.Errorf("%s is only supported on macOS", authLoginKeychainFileName)
	}
	cmd := exec.Command("secret-tool", "store", "--label="+droidKeyringService, "service", droidKeyringService, "account", account) //nolint:gosec // Fixed arguments; the secret arrives over stdin.
	cmd.Stdin = strings.NewReader(encoded)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("store Droid encryption key in OS keyring: %w: %s", err, compactSpace(string(out)))
	}
	return nil
}

func macOSKeychainAddCommand(service, account, secret string) (string, error) {
	quoted := make([]string, 3)
	for i, value := range []string{service, account, secret} {
		if strings.ContainsAny(value, "\"\\\r\n\x00") {
			return "", fmt.Errorf("cannot write unsupported characters to the macOS Login Keychain")
		}
		quoted[i] = `"` + value + `"`
	}
	return fmt.Sprintf("add-generic-password -s %s -a %s -w %s -U", quoted[0], quoted[1], quoted[2]), nil
}

// clearSystemKeyringKeys removes every OS secret store entry for Droid's key.
var clearSystemKeyringKeys = func(format authFormat) error {
	account, err := droidSecureStorageAccount(format)
	if err != nil {
		return err
	}
	if runtime.GOOS == "darwin" {
		cmd := exec.Command(macOSSecurityCLI, "delete-generic-password", "-s", droidKeyringService, "-a", account) //nolint:gosec // Fixed executable and arguments.
		if out, err := cmd.CombinedOutput(); err != nil {
			var exitErr *exec.ExitError
			if errors.As(err, &exitErr) && exitErr.ExitCode() == macOSSecurityCLIItemNotFoundStatus {
				return nil
			}
			return fmt.Errorf("clear Droid encryption key from macOS Login Keychain: %w: %s", err, compactSpace(string(out)))
		}
		return nil
	}
	if runtime.GOOS != "linux" {
		return fmt.Errorf("droid secure credential storage is unsupported on %s", runtime.GOOS)
	}
	if format != authFormatKeyring {
		return fmt.Errorf("%s is only supported on macOS", authLoginKeychainFileName)
	}
	cmd := exec.Command("secret-tool", "clear", "service", droidKeyringService, "account", account) //nolint:gosec // Fixed arguments.
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("clear Droid encryption keys from OS keyring: %w: %s", err, compactSpace(string(out)))
	}
	return nil
}

// readAllSystemKeyringKeys lists every OS secret store entry for Droid's key.
// Some secret-service backends (ksecretd) keep duplicate items for the same
// attributes, and plain lookups may then return a stale entry; Droid itself
// adds a duplicate when it generates a fresh key after a keyring read failure.
var readAllSystemKeyringKeys = func(format authFormat) ([][]byte, error) {
	if runtime.GOOS == "darwin" {
		key, err := readSystemKeyringKey(format)
		if err != nil {
			return nil, err
		}
		return [][]byte{key}, nil
	}
	if runtime.GOOS != "linux" {
		return nil, fmt.Errorf("droid secure credential storage is unsupported on %s", runtime.GOOS)
	}
	if format != authFormatKeyring {
		return nil, fmt.Errorf("%s is only supported on macOS", authLoginKeychainFileName)
	}
	out, err := exec.Command("secret-tool", "search", "--all", "--unlock", "service", droidKeyringService, "account", droidKeyringAccount()).Output() //nolint:gosec // Fixed arguments; secrets stay in process memory.
	if err != nil {
		return nil, fmt.Errorf("list Droid encryption keys in OS keyring: %w", err)
	}
	keys := parseSecretToolSearchOutput(string(out))
	if len(keys) == 0 {
		return nil, fmt.Errorf("no Droid encryption key entries found in OS keyring")
	}
	return keys, nil
}

// parseSecretToolSearchOutput extracts Droid AES keys from secret-tool search
// output. The command-line attribute filter already scoped the search, and
// some backends (ksecretd) omit attribute lines from the output entirely, so
// a block is only rejected when one of its attributes explicitly contradicts
// the search. Blocks without a valid secret line contribute nothing.
func parseSecretToolSearchOutput(out string) [][]byte {
	var keys [][]byte
	seen := map[string]bool{}
	var block strings.Builder
	flush := func() {
		defer block.Reset()
		text := block.String()
		if service, ok := searchOutputAttribute(text, "service"); ok && service != droidKeyringService {
			return
		}
		if account, ok := searchOutputAttribute(text, "account"); ok && account != droidKeyringAccount() {
			return
		}
		for line := range strings.SplitSeq(text, "\n") {
			value, ok := strings.CutPrefix(strings.TrimSpace(line), "secret = ")
			if !ok {
				continue
			}
			key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(value))
			if err != nil || len(key) != droidAuthKeySize {
				return
			}
			if !seen[string(key)] {
				seen[string(key)] = true
				keys = append(keys, key)
			}
			return
		}
	}
	for line := range strings.SplitSeq(out, "\n") {
		if strings.HasPrefix(line, "[/") {
			flush()
		}
		block.WriteString(line + "\n")
	}
	flush()
	return keys
}

// searchOutputAttribute reads one "attribute.<name> = <value>" line from a
// secret-tool search result block.
func searchOutputAttribute(block, name string) (string, bool) {
	prefix := "attribute." + name + " = "
	for line := range strings.SplitSeq(block, "\n") {
		if value, ok := strings.CutPrefix(strings.TrimSpace(line), prefix); ok {
			return strings.TrimSpace(value), true
		}
	}
	return "", false
}

// writeSystemKeyringKey stores Droid's AES key in the OS secret store so the
// live Droid binary can decrypt an activated keyring-format account. Stale
// duplicates are removed first and the result is verified, because a backend
// that keeps multiple entries may answer lookups with the wrong key.
var writeSystemKeyringKey = func(format authFormat, key []byte) error {
	return writeSystemKeyringKeyWithClear(format, key, runtime.GOOS == "linux")
}

func writeSystemKeyringKeyWithClear(format authFormat, key []byte, clearFirst bool) error {
	// A clear failure must not block the first-ever store; the post-store
	// lookup below still verifies what Droid will read back. Linux needs the
	// clear to collapse duplicate Secret Service entries; macOS security -U
	// updates the unique matching Login Keychain item without a delete window.
	if clearFirst {
		_ = clearSystemKeyringKeys(format)
	}
	if err := storeSystemKeyringKey(format, key); err != nil {
		return err
	}
	got, err := readSystemKeyringKey(format)
	if err != nil {
		return fmt.Errorf("verify Droid encryption key in OS keyring after store: %w", err)
	}
	if !bytes.Equal(got, key) {
		account, _ := droidSecureStorageAccount(format)
		return fmt.Errorf("OS keyring still returns a stale Droid encryption key after rewrite; remove duplicate %q/%q entries manually", droidKeyringService, account)
	}
	return nil
}

// keyringKeyDecryptsHome reports whether key decrypts the home's keyring file.
func keyringKeyDecryptsHome(factoryHome string, format authFormat, key []byte) bool {
	// factoryHome is always a switcher-owned or Factory-owned home, never raw user input.
	encrypted, err := os.ReadFile(filepath.Join(factoryHome, authCredentialsFileName(format))) //nolint:gosec // Path is scoped by validated account storage.
	if err != nil {
		return false
	}
	_, err = decryptDroidCredentials(strings.TrimSpace(string(encrypted)), key)
	return err == nil
}

// snapshotSystemKeyringKey returns the OS keyring key that actually decrypts
// the given keyring-format home. The plain lookup result is verified against
// the ciphertext; on mismatch every keyring entry is tried, which recovers
// from duplicate entries left behind when Droid generated a fresh key.
func snapshotSystemKeyringKey(factoryHome string, format authFormat) ([]byte, error) {
	key, lookupErr := readSystemKeyringKey(format)
	if lookupErr == nil && keyringKeyDecryptsHome(factoryHome, format, key) {
		return key, nil
	}
	candidates, err := readAllSystemKeyringKeys(format)
	if err != nil {
		if lookupErr != nil {
			return nil, fmt.Errorf("read Droid encryption key from OS keyring: %w (listing all entries also failed: %w)", lookupErr, err)
		}
		return nil, err
	}
	for _, candidate := range candidates {
		if keyringKeyDecryptsHome(factoryHome, format, candidate) {
			return candidate, nil
		}
	}
	return nil, fmt.Errorf("no OS keyring entry decrypts %s; the key Droid used is missing from the keyring", authCredentialsFileName(format))
}

// droidKeyringAccount mirrors Droid's keyring item name, including its dev suffix.
func droidKeyringAccount() string {
	if strings.EqualFold(os.Getenv("FACTORY_ENV"), "development") {
		return droidKeyringAccountProd + "-dev"
	}
	return droidKeyringAccountProd
}

func droidLoginKeychainAccount() string {
	if strings.EqualFold(os.Getenv("FACTORY_ENV"), "development") {
		return droidLoginKeychainAccountProd + "-dev"
	}
	return droidLoginKeychainAccountProd
}

func droidSecureStorageAccount(format authFormat) (string, error) {
	switch format {
	case authFormatKeyring:
		return droidKeyringAccount(), nil
	case authFormatLoginKeychain:
		return droidLoginKeychainAccount(), nil
	default:
		return "", fmt.Errorf("auth format %s does not use secure OS storage", authFormatName(format))
	}
}

// detectAuthFormat recognizes Droid's platform-specific secure formats before
// falling back to keyfile-v2. Normal switcher operations keep only one format.
func detectAuthFormat(factoryHome string) authFormat {
	if nonEmptyFile(filepath.Join(factoryHome, authLoginKeychainFileName)) {
		return authFormatLoginKeychain
	}
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
	loginKeychainPath := filepath.Join(factoryHome, authLoginKeychainFileName)
	if info, err := os.Stat(loginKeychainPath); err == nil && !info.IsDir() {
		return authFormatNone, fmt.Errorf("%s is empty", authLoginKeychainFileName)
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
		return authFormatNone, fmt.Errorf("no Droid login found (none of %s, %s, or %s/%s exist)", authLoginKeychainFileName, authKeyringFileName, authFileName, authKeyFileName)
	}
}

// authFilesForFormat lists the Droid-owned files that make up a complete auth
// set for a format. The switcher-owned keyring key snapshot is handled
// separately because it never lives in the real Factory home.
func authFilesForFormat(format authFormat) []string {
	switch format {
	case authFormatLoginKeychain:
		return []string{authLoginKeychainFileName}
	case authFormatKeyring:
		return []string{authKeyringFileName}
	case authFormatKeyfile:
		return []string{authFileName, authKeyFileName}
	default:
		return nil
	}
}

// allDroidAuthFileNames lists every Droid-owned auth file name across formats.
var allDroidAuthFileNames = []string{authFileName, authKeyFileName, authKeyringFileName, authLoginKeychainFileName}

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
	if keep != authFormatLoginKeychain {
		if err := removeFileIfExists(filepath.Join(factoryHome, authLoginKeychainKeyFileName)); err != nil {
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
func readSavedKeyringKey(factoryHome string, format authFormat) ([]byte, error) {
	keyFileName, err := savedSecureKeyFileName(format)
	if err != nil {
		return nil, err
	}
	// factoryHome is always a switcher-owned account home, never raw user input.
	raw, err := os.ReadFile(filepath.Join(factoryHome, keyFileName)) //nolint:gosec // Path is scoped by validated account storage.
	if err != nil {
		return nil, err
	}
	key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(raw)))
	if err != nil {
		return nil, fmt.Errorf("decode saved Droid secure-storage key: %w", err)
	}
	if len(key) != droidAuthKeySize {
		return nil, fmt.Errorf("invalid saved Droid secure-storage key length: got %d, want %d", len(key), droidAuthKeySize)
	}
	return key, nil
}

// writeSavedKeyringKey stores the keyring key snapshot inside a saved account home.
func writeSavedKeyringKey(factoryHome string, format authFormat, key []byte) error {
	keyFileName, err := savedSecureKeyFileName(format)
	if err != nil {
		return err
	}
	return atomicWriteFile(filepath.Join(factoryHome, keyFileName), []byte(base64.StdEncoding.EncodeToString(key)), 0o600)
}

func savedSecureKeyFileName(format authFormat) (string, error) {
	switch format {
	case authFormatKeyring:
		return authKeyringKeyFileName, nil
	case authFormatLoginKeychain:
		return authLoginKeychainKeyFileName, nil
	default:
		return "", fmt.Errorf("auth format %s does not use a saved secure-storage key", authFormatName(format))
	}
}

func usesSecureStorage(format authFormat) bool {
	return format == authFormatKeyring || format == authFormatLoginKeychain
}
