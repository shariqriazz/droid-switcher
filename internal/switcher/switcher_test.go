package switcher

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestCleanAccountName(t *testing.T) {
	valid := []string{"work", "personal.1", "team-prod", "acct_2"}
	for _, name := range valid {
		got, err := CleanAccountName(" " + name + " ")
		if err != nil {
			t.Fatalf("CleanAccountName(%q) returned error: %v", name, err)
		}
		if got != name {
			t.Fatalf("CleanAccountName(%q) = %q", name, got)
		}
	}

	invalid := []string{"", ".", "..", "../x", "team/prod", "bad name", "x:y"}
	for _, name := range invalid {
		if _, err := CleanAccountName(name); err == nil {
			t.Fatalf("CleanAccountName(%q) returned nil error", name)
		}
	}
}

func TestResolveAccountNameGeneratesWhenBlank(t *testing.T) {
	p := NewPaths(t.TempDir())
	name, generated, err := ResolveAccountName(p, "")
	if err != nil {
		t.Fatal(err)
	}
	if !generated {
		t.Fatal("expected generated name")
	}
	if !strings.HasPrefix(name, "account-") {
		t.Fatalf("generated name %q missing expected prefix", name)
	}
}

func TestSaveCurrentAndSwitchAccount(t *testing.T) {
	p := NewPaths(t.TempDir())
	writeAuth(t, p.FactoryHome, "current-file", "current-key")

	var out bytes.Buffer
	if err := SaveCurrent(p, "current", SaveOptions{Label: "Current Label"}, &out); err != nil {
		t.Fatal(err)
	}
	writeAuth(t, p.AccountFactoryHome("other"), "other-file", "other-key")

	out.Reset()
	if err := SwitchAccount(p, "other", &out); err != nil {
		t.Fatal(err)
	}

	assertFile(t, filepath.Join(p.FactoryHome, authFileName), "other-file")
	assertFile(t, filepath.Join(p.FactoryHome, authKeyFileName), "other-key")
	assertFile(t, p.ActiveFile, "other\n")

	backups, err := filepath.Glob(filepath.Join(p.Store, "backups", "*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(backups) != 1 {
		t.Fatalf("expected one backup directory, got %d", len(backups))
	}
}

func TestSelectAccountByNumber(t *testing.T) {
	p := NewPaths(t.TempDir())
	writeAuth(t, p.AccountFactoryHome("alpha"), "a", "a-key")
	writeAuth(t, p.AccountFactoryHome("beta"), "b", "b-key")

	var out bytes.Buffer
	got, err := SelectAccount(p, strings.NewReader("2\n"), &out)
	if err != nil {
		t.Fatal(err)
	}
	if got != "beta" {
		t.Fatalf("selected %q, want beta", got)
	}
	if !strings.Contains(out.String(), "Select a Droid account") {
		t.Fatalf("selection prompt missing: %q", out.String())
	}
}

func TestSelectAccountByUniqueLabel(t *testing.T) {
	p := NewPaths(t.TempDir())
	writeAuth(t, p.AccountFactoryHome("alpha"), "a", "a-key")
	writeAuth(t, p.AccountFactoryHome("beta"), "b", "b-key")
	if err := saveAccountMetadata(p, "beta", AccountMetadata{Label: "Team Beta"}); err != nil {
		t.Fatal(err)
	}
	got, err := SelectAccount(p, strings.NewReader("Team Beta\n"), &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	if got != "beta" {
		t.Fatalf("selected %q, want beta", got)
	}
}

func TestQuotaRunsDroidLimitsForActiveAccount(t *testing.T) {
	p := NewPaths(t.TempDir())
	writeAuth(t, p.AccountFactoryHome("work"), "file", "key")
	if err := saveAccountMetadata(p, "work", AccountMetadata{Label: "Work Label"}); err != nil {
		t.Fatal(err)
	}
	if err := atomicWriteFile(p.ActiveFile, []byte("work\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	var gotHome string
	var gotArgs []string
	oldRunDroid := runDroid
	runDroid = func(r DroidRunner, factoryHome string, args ...string) error {
		gotHome = factoryHome
		gotArgs = append([]string(nil), args...)
		return nil
	}
	defer func() { runDroid = oldRunDroid }()

	var out bytes.Buffer
	if err := Quota(p, QuotaOptions{Droid: "droid-test"}, &out); err != nil {
		t.Fatal(err)
	}
	if gotHome != p.AccountFactoryHome("work") {
		t.Fatalf("factory home = %q, want %q", gotHome, p.AccountFactoryHome("work"))
	}
	wantArgs := []string{"exec", "--output-format", "text", "/limits"}
	if !reflect.DeepEqual(gotArgs, wantArgs) {
		t.Fatalf("args = %#v, want %#v", gotArgs, wantArgs)
	}
	if !strings.Contains(out.String(), "Work Label (work)") {
		t.Fatalf("expected label in quota output, got %q", out.String())
	}
}

func TestQuotaAllWithExplicitAccountErrors(t *testing.T) {
	err := Quota(NewPaths(t.TempDir()), QuotaOptions{All: true, Account: "work"}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "cannot combine --all") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParseLimitWindows(t *testing.T) {
	raw := `
Plan: Pro
- 5h: 10 / 50 used, resets soon
- 1wk: 120 / 500 used
- 1month: 700 / 2000 used
`
	got := parseLimitWindows(raw)
	want := []LimitWindow{
		{Window: "5h", Text: "5h: 10 / 50 used, resets soon"},
		{Window: "1wk", Text: "1wk: 120 / 500 used"},
		{Window: "1month", Text: "1month: 700 / 2000 used"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseLimitWindows = %#v, want %#v", got, want)
	}
}

func TestSaveCurrentRequiresForceForExistingAccount(t *testing.T) {
	p := NewPaths(t.TempDir())
	writeAuth(t, p.FactoryHome, "one", "one-key")
	var out bytes.Buffer
	if err := SaveCurrent(p, "work", SaveOptions{}, &out); err != nil {
		t.Fatal(err)
	}
	writeAuth(t, p.FactoryHome, "two", "two-key")
	if err := SaveCurrent(p, "work", SaveOptions{}, &out); err == nil {
		t.Fatal("expected duplicate save to fail without force")
	}
	if err := SaveCurrent(p, "work", SaveOptions{Force: true}, &out); err != nil {
		t.Fatal(err)
	}
	assertFile(t, filepath.Join(p.AccountFactoryHome("work"), authFileName), "two")
}

func TestSaveCurrentForcePreservesExistingLabel(t *testing.T) {
	p := NewPaths(t.TempDir())
	writeAuth(t, p.FactoryHome, "one", "one-key")
	if err := SaveCurrent(p, "work", SaveOptions{Label: "Work Label"}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	writeAuth(t, p.FactoryHome, "two", "two-key")
	if err := SaveCurrent(p, "work", SaveOptions{Force: true}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	meta, err := loadAccountMetadata(p, "work")
	if err != nil {
		t.Fatal(err)
	}
	if meta.Label != "Work Label" {
		t.Fatalf("label = %q", meta.Label)
	}
}

func TestSaveCurrentBlankNameGenerates(t *testing.T) {
	p := NewPaths(t.TempDir())
	writeAuth(t, p.FactoryHome, "one", "one-key")
	var out bytes.Buffer
	if err := SaveCurrent(p, "", SaveOptions{}, &out); err != nil {
		t.Fatal(err)
	}
	accounts, err := ListAccounts(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(accounts) != 1 {
		t.Fatalf("expected 1 account, got %d", len(accounts))
	}
	if !strings.HasPrefix(accounts[0].Name, "account-") {
		t.Fatalf("generated account name %q missing prefix", accounts[0].Name)
	}
	if !strings.Contains(out.String(), "Using generated name") {
		t.Fatalf("expected generated-name message, got %q", out.String())
	}
}

func TestRenameAccountUpdatesActiveMarker(t *testing.T) {
	p := NewPaths(t.TempDir())
	writeAuth(t, p.AccountFactoryHome("old"), "file", "key")
	if err := writeDefaultAccount(p, "old"); err != nil {
		t.Fatal(err)
	}
	if err := atomicWriteFile(p.ActiveFile, []byte("old\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := RenameAccount(p, "old", "new", &out); err != nil {
		t.Fatal(err)
	}
	assertFile(t, p.ActiveFile, "new\n")
	if err := EnsureAuth(p.AccountFactoryHome("new")); err != nil {
		t.Fatal(err)
	}
	defaultName, ok, err := CurrentDefaultAccount(p)
	if err != nil || !ok || defaultName != "new" {
		t.Fatalf("unexpected default after rename: %q %v %v", defaultName, ok, err)
	}
}

func TestSwitchWithoutNameSelectsInteractively(t *testing.T) {
	p := NewPaths(t.TempDir())
	writeAuth(t, p.AccountFactoryHome("alpha"), "file", "key")

	var out, errOut bytes.Buffer
	cli := CLI{Paths: p, Stdin: strings.NewReader("alpha\n"), Stdout: &out, Stderr: &errOut}
	if err := cli.Run([]string{"droid-switcher", "switch"}); err != nil {
		t.Fatal(err)
	}
	assertFile(t, p.ActiveFile, "alpha\n")
}

func TestLoginRequiresForceForExistingAccount(t *testing.T) {
	p := NewPaths(t.TempDir())
	writeAuth(t, p.AccountFactoryHome("work"), "file", "key")
	oldRunDroid := runDroid
	runDroid = func(r DroidRunner, factoryHome string, args ...string) error { return nil }
	defer func() { runDroid = oldRunDroid }()
	err := Login(p, "work", "droid-test", "", false, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "pass --force") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoginForcePreservesExistingLabel(t *testing.T) {
	p := NewPaths(t.TempDir())
	writeAuth(t, p.AccountFactoryHome("work"), "old-file", "old-key")
	if err := saveAccountMetadata(p, "work", AccountMetadata{Label: "Existing Label"}); err != nil {
		t.Fatal(err)
	}
	oldRunDroid := runDroid
	runDroid = func(r DroidRunner, factoryHome string, args ...string) error {
		writeAuth(t, factoryHome, "new-file", "new-key")
		return nil
	}
	defer func() { runDroid = oldRunDroid }()
	if err := Login(p, "work", "droid-test", "", true, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	meta, err := loadAccountMetadata(p, "work")
	if err != nil {
		t.Fatal(err)
	}
	if meta.Label != "Existing Label" {
		t.Fatalf("label = %q", meta.Label)
	}
}

func TestSyncCurrentAuthToSavedAccount(t *testing.T) {
	p := NewPaths(t.TempDir())
	writeAuth(t, p.AccountFactoryHome("work"), "old-file", "old-key")
	writeAuth(t, p.FactoryHome, "new-file", "new-key")
	if err := atomicWriteFile(p.ActiveFile, []byte("work\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := SyncCurrentAuthToSavedAccount(p, &out); err != nil {
		t.Fatal(err)
	}
	assertFile(t, filepath.Join(p.AccountFactoryHome("work"), authFileName), "new-file")
	assertFile(t, filepath.Join(p.AccountFactoryHome("work"), authKeyFileName), "new-key")
	if !strings.Contains(out.String(), "Synced current auth") {
		t.Fatalf("unexpected output: %q", out.String())
	}
}

func TestRemoveAccountMissingErrors(t *testing.T) {
	err := RemoveAccount(NewPaths(t.TempDir()), "ghost", &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestRemoveAccountClearsMarkers(t *testing.T) {
	p := NewPaths(t.TempDir())
	writeAuth(t, p.AccountFactoryHome("work"), "file", "key")
	if err := atomicWriteFile(p.ActiveFile, []byte("work\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeDefaultAccount(p, "work"); err != nil {
		t.Fatal(err)
	}
	if err := RemoveAccount(p, "work", &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := CurrentAccount(p); ok {
		t.Fatal("expected active marker cleared")
	}
	if _, ok, _ := CurrentDefaultAccount(p); ok {
		t.Fatal("expected default marker cleared")
	}
}

func TestSetLabelAndDefault(t *testing.T) {
	p := NewPaths(t.TempDir())
	writeAuth(t, p.AccountFactoryHome("alpha"), "file", "key")
	var out bytes.Buffer
	if err := SetAccountLabel(p, "alpha", "Alpha Label", &out); err != nil {
		t.Fatal(err)
	}
	if err := SetDefaultAccount(p, "alpha", &out); err != nil {
		t.Fatal(err)
	}
	accounts, err := ListAccounts(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(accounts) != 1 || accounts[0].Label != "Alpha Label" || !accounts[0].Default {
		t.Fatalf("unexpected accounts: %#v", accounts)
	}
}

func TestNoArgsOpensMenu(t *testing.T) {
	p := NewPaths(t.TempDir())
	writeAuth(t, p.AccountFactoryHome("alpha"), "file", "key")
	var out, errOut bytes.Buffer
	cli := CLI{Paths: p, Stdin: strings.NewReader("10\n"), Stdout: &out, Stderr: &errOut}
	if err := cli.Run([]string{"droid-switcher"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Droid Switcher") || !strings.Contains(out.String(), "What do you want to do?") {
		t.Fatalf("unexpected menu output: %q", out.String())
	}
}

func TestPrintCurrentFallsBackToDefault(t *testing.T) {
	p := NewPaths(t.TempDir())
	writeAuth(t, p.AccountFactoryHome("alpha"), "file", "key")
	if err := saveAccountMetadata(p, "alpha", AccountMetadata{Label: "Alpha Label"}); err != nil {
		t.Fatal(err)
	}
	if err := writeDefaultAccount(p, "alpha"); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := printCurrent(p, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "default: Alpha Label (alpha)") {
		t.Fatalf("unexpected output: %q", out.String())
	}
}

func TestPromptMenuChoiceErrors(t *testing.T) {
	_, err := promptMenuChoice(strings.NewReader("x\n"), &bytes.Buffer{}, "Title", []string{"a"})
	if err == nil {
		t.Fatal("expected invalid choice error")
	}
	_, err = promptMenuChoice(strings.NewReader("2\n"), &bytes.Buffer{}, "Title", []string{"a"})
	if err == nil {
		t.Fatal("expected out of range error")
	}
}

func TestVersionCommand(t *testing.T) {
	var out, errOut bytes.Buffer
	cli := CLI{Paths: NewPaths(t.TempDir()), Stdin: strings.NewReader(""), Stdout: &out, Stderr: &errOut}
	if err := cli.Run([]string{"droid-switcher", "version"}); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out.String()) != version {
		t.Fatalf("version output = %q", out.String())
	}
}

func TestRemoveRequiresYesOutsideInteractiveTerminal(t *testing.T) {
	var out, errOut bytes.Buffer
	cli := CLI{Paths: NewPaths(t.TempDir()), Stdin: strings.NewReader(""), Stdout: &out, Stderr: &errOut}
	err := cli.Run([]string{"droid-switcher", "remove", "ghost"})
	if err == nil || !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEmptyListAndQuotaError(t *testing.T) {
	p := NewPaths(t.TempDir())
	var out bytes.Buffer
	if err := printAccountList(p, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "No accounts saved.") {
		t.Fatalf("unexpected list output: %q", out.String())
	}
	if err := Quota(p, QuotaOptions{All: true}, &bytes.Buffer{}); err == nil {
		t.Fatal("expected quota error when no accounts exist")
	}
}

func writeAuth(t *testing.T, factoryHome, fileContent, keyContent string) {
	t.Helper()
	if err := os.MkdirAll(factoryHome, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(factoryHome, authFileName), []byte(fileContent), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(factoryHome, authKeyFileName), []byte(keyContent), 0o600); err != nil {
		t.Fatal(err)
	}
}

func assertFile(t *testing.T, path, want string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Fatalf("%s = %q, want %q", path, string(got), want)
	}
}
