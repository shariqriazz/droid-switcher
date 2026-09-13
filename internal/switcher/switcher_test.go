package switcher

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
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

func TestQuotaFetchesFactoryLimitsForActiveAccount(t *testing.T) {
	p := NewPaths(t.TempDir())
	writeEncryptedAuth(t, p.AccountFactoryHome("work"), droidCredentials{
		AccessToken:          testJWT(time.Now().Add(time.Hour)),
		RefreshToken:         "refresh-token",
		ActiveOrganizationID: "org-work",
	})
	if err := saveAccountMetadata(p, "work", AccountMetadata{Label: "Work Label"}); err != nil {
		t.Fatal(err)
	}
	if err := atomicWriteFile(p.ActiveFile, []byte("work\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	calls := 0
	withQuotaServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/api/billing/limits" {
			t.Fatalf("path = %q, want /api/billing/limits", r.URL.Path)
		}
		if got := r.Header.Get(factoryOrgHeader); got != "org-work" {
			t.Fatalf("org header = %q, want org-work", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"usesTokenRateLimitsBilling":true,"limits":{"standard":{"fiveHour":{"usedPercent":10,"secondsRemaining":3600},"weekly":{"usedPercent":20,"secondsRemaining":7200},"monthly":{"usedPercent":30,"secondsRemaining":10800}}}}`))
	}))

	var out bytes.Buffer
	if err := Quota(p, QuotaOptions{Droid: "droid-test"}, &out); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("limits calls = %d, want 1", calls)
	}
	if !strings.Contains(out.String(), "Work Label (work)") {
		t.Fatalf("expected label in quota output, got %q", out.String())
	}
	if !strings.Contains(out.String(), "standard") {
		t.Fatalf("expected standard group header, got %q", out.String())
	}
	if !strings.Contains(out.String(), "5h     10% used, resets in 1h") {
		t.Fatalf("expected formatted quota output, got %q", out.String())
	}
}

func TestQuotaDisplaysCoreGroupIdleWindowsAndExtraBalance(t *testing.T) {
	p := NewPaths(t.TempDir())
	writeEncryptedAuth(t, p.AccountFactoryHome("work"), droidCredentials{
		AccessToken:  testJWT(time.Now().Add(time.Hour)),
		RefreshToken: "refresh-token",
	})

	withQuotaServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/billing/limits" {
			t.Fatalf("path = %q, want /api/billing/limits", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"usesTokenRateLimitsBilling": true,
			"limits": {
				"standard": {
					"fiveHour": {"usedPercent": 10, "secondsRemaining": 3600},
					"weekly": {"usedPercent": 20, "secondsRemaining": 7200},
					"monthly": {"usedPercent": 30, "secondsRemaining": 10800}
				},
				"core": {
					"fiveHour": {"usedPercent": 17, "windowEnd": "2026-07-04T20:59:01.705Z", "secondsRemaining": null},
					"weekly": {"usedPercent": 12, "windowEnd": "2026-07-06T15:42:26.575Z", "secondsRemaining": null},
					"monthly": {"usedPercent": 4, "windowEnd": "2026-08-03T15:59:01.705Z", "secondsRemaining": null}
				}
			},
			"extraUsageAllowed": true,
			"extraUsageBalanceCents": 1234,
			"overagePreference": "droidCore"
		}`))
	}))

	var out bytes.Buffer
	if err := Quota(p, QuotaOptions{Account: "work"}, &out); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"standard",
		"core",
		"10% used, resets in 1h",
		"idle (last window: 17% used)",
		"idle (last window: 12% used)",
		"idle (last window: 4% used)",
		"extra usage balance: $12.34",
	} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("expected %q in quota output, got %q", want, out.String())
		}
	}
}

func TestQuotaAllWithExplicitAccountErrors(t *testing.T) {
	err := Quota(NewPaths(t.TempDir()), QuotaOptions{All: true, Account: "work"}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "cannot combine --all") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCLIQuotaShortCommandAndFlags(t *testing.T) {
	p := NewPaths(t.TempDir())
	writeEncryptedAuth(t, p.AccountFactoryHome("alpha"), droidCredentials{
		AccessToken:  testJWT(time.Now().Add(time.Hour)),
		RefreshToken: "alpha-refresh",
	})
	writeEncryptedAuth(t, p.AccountFactoryHome("beta"), droidCredentials{
		AccessToken:  testJWT(time.Now().Add(time.Hour)),
		RefreshToken: "beta-refresh",
	})

	calls := 0
	withQuotaServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/api/billing/limits" {
			t.Fatalf("path = %q, want /api/billing/limits", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"usesTokenRateLimitsBilling":true,"limits":{"standard":{"fiveHour":{"usedPercent":1},"weekly":{"usedPercent":2},"monthly":{"usedPercent":3}}}}`))
	}))

	var out, errOut bytes.Buffer
	cli := CLI{Paths: p, Stdin: strings.NewReader(""), Stdout: &out, Stderr: &errOut}
	if err := cli.Run([]string{"drsw", "q", "-a", "-r"}); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("runDroid calls = %d, want 2", calls)
	}
	if !strings.Contains(out.String(), "alpha") || !strings.Contains(out.String(), "beta") {
		t.Fatalf("expected both accounts in output, got %q", out.String())
	}
	if !strings.Contains(out.String(), "raw:") {
		t.Fatalf("expected raw output from -r, got %q", out.String())
	}
}

func TestCLIQuotaParsesRawAfterAccount(t *testing.T) {
	p := NewPaths(t.TempDir())
	writeEncryptedAuth(t, p.AccountFactoryHome("alpha"), droidCredentials{
		AccessToken:  testJWT(time.Now().Add(time.Hour)),
		RefreshToken: "alpha-refresh",
	})
	withQuotaServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/billing/limits" {
			t.Fatalf("path = %q, want /api/billing/limits", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"usesTokenRateLimitsBilling":true,"limits":{"standard":{"fiveHour":{"usedPercent":7},"weekly":{"usedPercent":8},"monthly":{"usedPercent":9}}}}`))
	}))

	var out, errOut bytes.Buffer
	cli := CLI{Paths: p, Stdin: strings.NewReader(""), Stdout: &out, Stderr: &errOut}
	if err := cli.Run([]string{"drsw", "q", "alpha", "-r"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "raw:") || !strings.Contains(out.String(), "7% used") {
		t.Fatalf("expected quota and raw output, got %q", out.String())
	}
}

func TestQuotaRefreshesExpiredAccessToken(t *testing.T) {
	p := NewPaths(t.TempDir())
	writeEncryptedAuth(t, p.AccountFactoryHome("work"), droidCredentials{
		AccessToken:          testJWT(time.Now().Add(-time.Hour)),
		RefreshToken:         "old-refresh",
		ActiveOrganizationID: "org-work",
	})
	if err := atomicWriteFile(p.ActiveFile, []byte("work\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	newAccess := testJWT(time.Now().Add(time.Hour))
	var refreshed, fetched bool
	withQuotaServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/authenticate":
			refreshed = true
			if err := r.ParseForm(); err != nil {
				t.Fatal(err)
			}
			if got := r.Form.Get("refresh_token"); got != "old-refresh" {
				t.Fatalf("refresh token = %q, want old-refresh", got)
			}
			if got := r.Form.Get("organization_id"); got != "" {
				t.Fatalf("organization id = %q, want empty (Droid omits it on routine refresh)", got)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":` + strconv.Quote(newAccess) + `,"refresh_token":"new-refresh"}`))
		case "/api/billing/limits":
			fetched = true
			if got := r.Header.Get("Authorization"); got != "Bearer "+newAccess {
				t.Fatalf("authorization header used stale token")
			}
			if got := r.Header.Get(factoryOrgHeader); got != "org-work" {
				t.Fatalf("org header = %q, want org-work", got)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"usesTokenRateLimitsBilling":true,"limits":{"standard":{"fiveHour":{"usedPercent":4},"weekly":{"usedPercent":5},"monthly":{"usedPercent":6}}}}`))
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	}))

	if err := Quota(p, QuotaOptions{Account: "work"}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if !refreshed || !fetched {
		t.Fatalf("refreshed=%v fetched=%v, want both true", refreshed, fetched)
	}
	creds, err := loadDroidCredentials(p.AccountFactoryHome("work"))
	if err != nil {
		t.Fatal(err)
	}
	if creds.AccessToken != newAccess || creds.RefreshToken != "new-refresh" || creds.ActiveOrganizationID != "org-work" {
		t.Fatalf("unexpected saved credentials after refresh: access=%v refresh=%q org=%q", creds.AccessToken == newAccess, creds.RefreshToken, creds.ActiveOrganizationID)
	}
	liveCreds, err := loadDroidCredentials(p.FactoryHome)
	if err != nil {
		t.Fatal(err)
	}
	if liveCreds.AccessToken != newAccess || liveCreds.RefreshToken != "new-refresh" || liveCreds.ActiveOrganizationID != "org-work" {
		t.Fatalf("unexpected live credentials after active refresh: access=%v refresh=%q org=%q", liveCreds.AccessToken == newAccess, liveCreds.RefreshToken, liveCreds.ActiveOrganizationID)
	}
}

func TestQuotaExplainsEndedLogin(t *testing.T) {
	p := NewPaths(t.TempDir())
	writeEncryptedAuth(t, p.AccountFactoryHome("bizkit"), droidCredentials{
		AccessToken:  testJWT(time.Now().Add(-time.Hour)),
		RefreshToken: "ended-refresh",
	})

	calls := 0
	withQuotaServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant","error_description":"Session has already ended."}`))
	}))

	err := Quota(p, QuotaOptions{Account: "bizkit"}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected ended login to fail")
	}
	if !strings.Contains(err.Error(), "droid-switcher login bizkit --force") {
		t.Fatalf("error does not explain recovery: %v", err)
	}
	if calls != 1 {
		t.Fatalf("refresh calls = %d, want 1 for a permanent error", calls)
	}
}

func TestDroidOverrideHomeUsesFactoryParent(t *testing.T) {
	p := NewPaths(t.TempDir())
	got := droidOverrideHome(p.AccountFactoryHome("work"))
	want := filepath.Join(p.Accounts, "work")
	if got != want {
		t.Fatalf("override home = %q, want %q", got, want)
	}

	custom := filepath.Join(t.TempDir(), "factory-home")
	if got := droidOverrideHome(custom); got != custom {
		t.Fatalf("custom override home = %q, want %q", got, custom)
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
	runDroid = func(_ DroidRunner, _ string, _ ...string) error { return nil }
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
	runDroid = func(_ DroidRunner, factoryHome string, _ ...string) error {
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

func TestLoginActivatesNewCredentials(t *testing.T) {
	tests := []struct {
		name          string
		loginAccount  string
		activeAccount string
		force         bool
	}{
		{
			name:          "new account",
			loginAccount:  "bizkit",
			activeAccount: "involens",
		},
		{
			name:          "replace active account",
			loginAccount:  "bizkit",
			activeAccount: "bizkit",
			force:         true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewPaths(t.TempDir())
			writeAuth(t, p.FactoryHome, "old-live-file", "old-live-key")
			writeAuth(t, p.AccountFactoryHome(tt.activeAccount), "old-saved-file", "old-saved-key")
			if err := atomicWriteFile(p.ActiveFile, []byte(tt.activeAccount+"\n"), 0o600); err != nil {
				t.Fatal(err)
			}

			oldRunDroid := runDroid
			runDroid = func(_ DroidRunner, factoryHome string, _ ...string) error {
				writeAuth(t, factoryHome, "fresh-login-file", "fresh-login-key")
				return nil
			}
			t.Cleanup(func() { runDroid = oldRunDroid })

			if err := Login(p, tt.loginAccount, "droid-test", "", tt.force, &bytes.Buffer{}); err != nil {
				t.Fatal(err)
			}

			assertFile(t, filepath.Join(p.FactoryHome, authFileName), "fresh-login-file")
			assertFile(t, filepath.Join(p.FactoryHome, authKeyFileName), "fresh-login-key")
			assertFile(t, filepath.Join(p.AccountFactoryHome(tt.loginAccount), authFileName), "fresh-login-file")
			assertFile(t, filepath.Join(p.AccountFactoryHome(tt.loginAccount), authKeyFileName), "fresh-login-key")
			assertFile(t, p.ActiveFile, tt.loginAccount+"\n")
			if tt.activeAccount != tt.loginAccount {
				assertFile(t, filepath.Join(p.AccountFactoryHome(tt.activeAccount), authFileName), "old-live-file")
				assertFile(t, filepath.Join(p.AccountFactoryHome(tt.activeAccount), authKeyFileName), "old-live-key")
			}
		})
	}
}

func TestLoginActivatesLoginKeychainCredentials(t *testing.T) {
	p := NewPaths(t.TempDir())
	key := bytes.Repeat([]byte{6}, droidAuthKeySize)
	stubSystemKeyring(t, key)

	oldRunDroid := runDroid
	runDroid = func(_ DroidRunner, factoryHome string, _ ...string) error {
		accessToken := strings.Join([]string{"fresh", "login", "access"}, "-")
		refreshToken := strings.Join([]string{"fresh", "login", "refresh"}, "-")
		writeLiveEncryptedLoginKeychainAuth(t, factoryHome, droidCredentials{
			AccessToken:  accessToken,
			RefreshToken: refreshToken,
		}, key)
		return nil
	}
	t.Cleanup(func() { runDroid = oldRunDroid })

	if err := Login(p, "work", "droid-test", "", false, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	assertFile(t, filepath.Join(p.AccountFactoryHome("work"), authLoginKeychainKeyFileName), base64.StdEncoding.EncodeToString(key))
	assertFile(t, p.ActiveFile, "work\n")
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

func TestSaveCurrentKeyringFormat(t *testing.T) {
	p := NewPaths(t.TempDir())
	key := bytes.Repeat([]byte{9}, droidAuthKeySize)
	writeLiveEncryptedKeyringAuth(t, p.FactoryHome, droidCredentials{
		AccessToken:  "live-access",
		RefreshToken: "live-refresh",
	}, key)
	stubSystemKeyring(t, key)

	var out bytes.Buffer
	if err := SaveCurrent(p, "work", SaveOptions{}, &out); err != nil {
		t.Fatal(err)
	}
	assertKeyringAuthDecrypts(t, p.AccountFactoryHome("work"), key, "live-access")
	assertFile(t, filepath.Join(p.AccountFactoryHome("work"), authKeyringKeyFileName), base64.StdEncoding.EncodeToString(key))
	if err := EnsureSavedAuth(p.AccountFactoryHome("work")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(p.AccountFactoryHome("work"), authFileName)); !os.IsNotExist(err) {
		t.Fatal("expected no keyfile credentials in a keyring-format saved account")
	}
}

func TestSaveCurrentLoginKeychainFormat(t *testing.T) {
	p := NewPaths(t.TempDir())
	key := bytes.Repeat([]byte{6}, droidAuthKeySize)
	writeLiveEncryptedLoginKeychainAuth(t, p.FactoryHome, droidCredentials{
		AccessToken:  "live-access",
		RefreshToken: "live-refresh",
	}, key)
	stubSystemKeyring(t, key)

	if err := SaveCurrent(p, "work", SaveOptions{}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	assertSecureAuthDecrypts(t, p.AccountFactoryHome("work"), authFormatLoginKeychain, key, "live-access")
	assertFile(t, filepath.Join(p.AccountFactoryHome("work"), authLoginKeychainKeyFileName), base64.StdEncoding.EncodeToString(key))
	if err := EnsureSavedAuth(p.AccountFactoryHome("work")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(p.AccountFactoryHome("work"), authKeyringFileName)); !os.IsNotExist(err) {
		t.Fatal("expected no Linux keyring credentials in a Login Keychain saved account")
	}
}

func TestSwitchAccountKeyringRestoresSystemKey(t *testing.T) {
	p := NewPaths(t.TempDir())
	key := bytes.Repeat([]byte{9}, droidAuthKeySize)
	writeEncryptedKeyringAuth(t, p.AccountFactoryHome("work"), droidCredentials{
		AccessToken:  "work-access",
		RefreshToken: "work-refresh",
	})
	writeAuth(t, p.FactoryHome, "live-file", "live-key")
	written := stubSystemKeyring(t, nil)

	var out bytes.Buffer
	if err := SwitchAccount(p, "work", &out); err != nil {
		t.Fatal(err)
	}
	assertKeyringAuthDecrypts(t, p.FactoryHome, key, "work-access")
	if !bytes.Equal(*written, key) {
		t.Fatal("expected the saved keyring key to be written to the OS keyring")
	}
	if _, err := os.Stat(filepath.Join(p.FactoryHome, authFileName)); !os.IsNotExist(err) {
		t.Fatal("expected stale keyfile auth displaced from the live home")
	}
	backups, err := filepath.Glob(filepath.Join(p.Store, "backups", "*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(backups) != 1 {
		t.Fatalf("expected one backup directory, got %d", len(backups))
	}
	assertFile(t, filepath.Join(backups[0], authFileName), "live-file")
}

func TestSwitchAccountLoginKeychainRestoresSystemKey(t *testing.T) {
	p := NewPaths(t.TempDir())
	key := bytes.Repeat([]byte{6}, droidAuthKeySize)
	writeLoginKeychainAuth(t, p.AccountFactoryHome("work"), droidCredentials{
		AccessToken:  "work-access",
		RefreshToken: "work-refresh",
	}, key)
	writeAuth(t, p.FactoryHome, "live-file", "live-key")
	written := stubSystemKeyring(t, nil)

	if err := SwitchAccount(p, "work", &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	assertSecureAuthDecrypts(t, p.FactoryHome, authFormatLoginKeychain, key, "work-access")
	if !bytes.Equal(*written, key) {
		t.Fatal("expected the saved Login Keychain key to be written to the OS key store")
	}
	if _, err := os.Stat(filepath.Join(p.FactoryHome, authFileName)); !os.IsNotExist(err) {
		t.Fatal("expected stale keyfile auth displaced from the live home")
	}
}

func TestSwitchAccountKeyfileDisplacesKeyring(t *testing.T) {
	p := NewPaths(t.TempDir())
	key := bytes.Repeat([]byte{9}, droidAuthKeySize)
	writeLiveEncryptedKeyringAuth(t, p.FactoryHome, droidCredentials{
		AccessToken:  "live-access",
		RefreshToken: "live-refresh",
	}, key)
	writeAuth(t, p.AccountFactoryHome("plain"), "plain-file", "plain-key")
	stubSystemKeyring(t, key)

	var out bytes.Buffer
	if err := SwitchAccount(p, "plain", &out); err != nil {
		t.Fatal(err)
	}
	assertFile(t, filepath.Join(p.FactoryHome, authFileName), "plain-file")
	assertFile(t, filepath.Join(p.FactoryHome, authKeyFileName), "plain-key")
	if _, err := os.Stat(filepath.Join(p.FactoryHome, authKeyringFileName)); !os.IsNotExist(err) {
		t.Fatal("expected keyring auth displaced from the live home")
	}
	backups, err := filepath.Glob(filepath.Join(p.Store, "backups", "*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(backups) != 1 {
		t.Fatalf("expected one backup directory, got %d", len(backups))
	}
	assertKeyringAuthDecrypts(t, backups[0], key, "live-access")
	assertFile(t, filepath.Join(backups[0], authKeyringKeyFileName), base64.StdEncoding.EncodeToString(key))
}

func TestSyncCurrentAuthConvertsSavedAccountFormat(t *testing.T) {
	p := NewPaths(t.TempDir())
	key := bytes.Repeat([]byte{9}, droidAuthKeySize)
	writeAuth(t, p.AccountFactoryHome("work"), "old-file", "old-key")
	writeLiveEncryptedKeyringAuth(t, p.FactoryHome, droidCredentials{
		AccessToken:  "live-access",
		RefreshToken: "live-refresh",
	}, key)
	if err := atomicWriteFile(p.ActiveFile, []byte("work\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	stubSystemKeyring(t, key)

	var out bytes.Buffer
	if err := SyncCurrentAuthToSavedAccount(p, &out); err != nil {
		t.Fatal(err)
	}
	assertKeyringAuthDecrypts(t, p.AccountFactoryHome("work"), key, "live-access")
	assertFile(t, filepath.Join(p.AccountFactoryHome("work"), authKeyringKeyFileName), base64.StdEncoding.EncodeToString(key))
	if _, err := os.Stat(filepath.Join(p.AccountFactoryHome("work"), authFileName)); !os.IsNotExist(err) {
		t.Fatal("expected stale keyfile auth removed from the saved account")
	}
	if !strings.Contains(out.String(), "Synced current auth") {
		t.Fatalf("unexpected output: %q", out.String())
	}
}

func TestEnsureSavedAuthRequiresKeyringKeySnapshot(t *testing.T) {
	p := NewPaths(t.TempDir())
	home := p.AccountFactoryHome("work")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, authKeyringFileName), []byte("cipher"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := EnsureSavedAuth(home)
	if err == nil || !strings.Contains(err.Error(), authKeyringKeyFileName) {
		t.Fatalf("expected missing key snapshot error, got %v", err)
	}
}

func TestEnsureSavedAuthRejectsUndecryptableKeyringSnapshot(t *testing.T) {
	p := NewPaths(t.TempDir())
	home := p.AccountFactoryHome("work")
	rightKey := bytes.Repeat([]byte{9}, droidAuthKeySize)
	wrongKey := bytes.Repeat([]byte{3}, droidAuthKeySize)
	encrypted, err := encryptDroidCredentials(droidCredentials{
		AccessToken:  "access",
		RefreshToken: "refresh",
	}, rightKey)
	if err != nil {
		t.Fatal(err)
	}
	// The snapshot holds a stale keyring entry's key, not the one Droid
	// encrypted the credentials with.
	writeKeyringAuth(t, home, encrypted, wrongKey)

	err = EnsureSavedAuth(home)
	if err == nil || !strings.Contains(err.Error(), "cannot be decrypted") {
		t.Fatalf("expected undecryptable snapshot error, got %v", err)
	}
}

func TestSaveCurrentKeyringRecoversKeyFromDuplicateEntries(t *testing.T) {
	p := NewPaths(t.TempDir())
	stale := bytes.Repeat([]byte{1}, droidAuthKeySize)
	current := bytes.Repeat([]byte{7}, droidAuthKeySize)
	writeLiveEncryptedKeyringAuth(t, p.FactoryHome, droidCredentials{
		AccessToken:  "live-access",
		RefreshToken: "live-refresh",
	}, current)
	// Droid stored a fresh key next to a stale one and plain lookups return
	// the stale entry, so the snapshot must come from trying every entry.
	oldRead, oldReadAll := readSystemKeyringKey, readAllSystemKeyringKeys
	readSystemKeyringKey = func(authFormat) ([]byte, error) { return stale, nil }
	readAllSystemKeyringKeys = func(authFormat) ([][]byte, error) { return [][]byte{stale, current}, nil }
	t.Cleanup(func() {
		readSystemKeyringKey = oldRead
		readAllSystemKeyringKeys = oldReadAll
	})

	var out bytes.Buffer
	if err := SaveCurrent(p, "work", SaveOptions{}, &out); err != nil {
		t.Fatal(err)
	}
	assertFile(t, filepath.Join(p.AccountFactoryHome("work"), authKeyringKeyFileName), base64.StdEncoding.EncodeToString(current))
	if err := EnsureSavedAuth(p.AccountFactoryHome("work")); err != nil {
		t.Fatal(err)
	}
}

func TestSaveCurrentKeyringFailsWhenNoEntryDecrypts(t *testing.T) {
	p := NewPaths(t.TempDir())
	stale := bytes.Repeat([]byte{1}, droidAuthKeySize)
	current := bytes.Repeat([]byte{7}, droidAuthKeySize)
	writeLiveEncryptedKeyringAuth(t, p.FactoryHome, droidCredentials{
		AccessToken:  "live-access",
		RefreshToken: "live-refresh",
	}, current)
	// Droid's key never reached the keyring at all: every entry is stale.
	oldRead, oldReadAll := readSystemKeyringKey, readAllSystemKeyringKeys
	readSystemKeyringKey = func(authFormat) ([]byte, error) { return stale, nil }
	readAllSystemKeyringKeys = func(authFormat) ([][]byte, error) { return [][]byte{stale}, nil }
	t.Cleanup(func() {
		readSystemKeyringKey = oldRead
		readAllSystemKeyringKeys = oldReadAll
	})

	var out bytes.Buffer
	err := SaveCurrent(p, "work", SaveOptions{}, &out)
	if err == nil || !strings.Contains(err.Error(), "no OS keyring entry decrypts") {
		t.Fatalf("expected missing key error, got %v", err)
	}
}

func TestWriteSystemKeyringKeyClearsStaleEntriesAndVerifies(t *testing.T) {
	key := bytes.Repeat([]byte{4}, droidAuthKeySize)
	stale := bytes.Repeat([]byte{8}, droidAuthKeySize)
	stored := stale
	cleared := false
	oldClear, oldStore, oldRead := clearSystemKeyringKeys, storeSystemKeyringKey, readSystemKeyringKey
	clearSystemKeyringKeys = func(authFormat) error {
		cleared = true
		stored = nil
		return nil
	}
	storeSystemKeyringKey = func(_ authFormat, k []byte) error {
		stored = k
		return nil
	}
	readSystemKeyringKey = func(authFormat) ([]byte, error) {
		if stored == nil {
			return nil, errors.New("OS keyring has no Droid key")
		}
		return stored, nil
	}
	t.Cleanup(func() {
		clearSystemKeyringKeys = oldClear
		storeSystemKeyringKey = oldStore
		readSystemKeyringKey = oldRead
	})

	if err := writeSystemKeyringKeyWithClear(authFormatKeyring, key, true); err != nil {
		t.Fatal(err)
	}
	if !cleared {
		t.Fatal("expected stale keyring entries to be cleared before storing")
	}

	// A backend that keeps answering lookups with a stale entry must fail
	// loudly instead of leaving the active account undecryptable for Droid.
	readSystemKeyringKey = func(authFormat) ([]byte, error) { return stale, nil }
	if err := writeSystemKeyringKeyWithClear(authFormatKeyring, key, true); err == nil || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("expected stale-entry verification error, got %v", err)
	}
}

func TestParseSecretToolSearchOutput(t *testing.T) {
	keyA := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{1}, droidAuthKeySize))
	keyB := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{7}, droidAuthKeySize))
	tests := []struct {
		name string
		out  string
		want int
	}{
		{"attributes present", "[/1]\nlabel = Factory CLI\nsecret = " + keyA + "\nattribute.account = auth-encryption-key\nattribute.service = Factory CLI\n", 1},
		// ksecretd omits attribute lines from search output entirely; the
		// command-line filter already scoped the search, so the block counts.
		{"attributes omitted by backend", "[/1]\nlabel = Factory CLI\nsecret = " + keyA + "\ncreated = 2026-08-05 12:46:07\n", 1},
		{"duplicates without attributes", "[/1]\nsecret = " + keyA + "\n[/2]\nsecret = " + keyB + "\n", 2},
		{"contradicting account skipped", "[/1]\nsecret = " + keyA + "\nattribute.account = other\nattribute.service = Factory CLI\n", 0},
		{"contradicting service skipped", "[/1]\nsecret = " + keyA + "\nattribute.account = auth-encryption-key\nattribute.service = Other\n", 0},
		{"duplicate secrets deduped", "[/1]\nsecret = " + keyA + "\n[/2]\nsecret = " + keyA + "\n", 1},
		{"invalid base64 skipped", "[/1]\nsecret = not-base64!!\n", 0},
		{"wrong key length skipped", "[/1]\nsecret = " + base64.StdEncoding.EncodeToString([]byte{1, 2, 3}) + "\n", 0},
		{"no secret line", "[/1]\nlabel = Factory CLI\n", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := len(parseSecretToolSearchOutput(tt.out)); got != tt.want {
				t.Fatalf("keys = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestMacOSKeychainAddCommandRejectsControlCharacters(t *testing.T) {
	if _, err := macOSKeychainAddCommand("Factory CLI", "account", "valid-base64"); err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"bad\nvalue", `bad"value`, `bad\value`} {
		if _, err := macOSKeychainAddCommand("Factory CLI", "account", value); err == nil {
			t.Fatalf("expected %q to be rejected", value)
		}
	}
}

func TestDoctorReportsHealthyKeyring(t *testing.T) {
	p := NewPaths(t.TempDir())
	key := bytes.Repeat([]byte{5}, droidAuthKeySize)
	writeLiveEncryptedKeyringAuth(t, p.FactoryHome, droidCredentials{
		AccessToken:  "live-access",
		RefreshToken: "live-refresh",
	}, key)
	stubSystemKeyring(t, key)

	var out bytes.Buffer
	if err := Doctor(p, DoctorOptions{}, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Keyring state is healthy") {
		t.Fatalf("expected healthy report, got %q", out.String())
	}
}

func TestDoctorHealCollapsesDuplicateEntries(t *testing.T) {
	p := NewPaths(t.TempDir())
	stale := bytes.Repeat([]byte{1}, droidAuthKeySize)
	current := bytes.Repeat([]byte{7}, droidAuthKeySize)
	writeLiveEncryptedKeyringAuth(t, p.FactoryHome, droidCredentials{
		AccessToken:  "live-access",
		RefreshToken: "live-refresh",
	}, current)
	// Droid generated a fresh key next to a stale one and plain lookups return
	// the stale entry, so Droid starts with a decrypt failure and asks for login.
	written := stubSystemKeyringEntries(t, [][]byte{stale, current})

	var out bytes.Buffer
	if err := Doctor(p, DoctorOptions{}, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "doctor --heal") {
		t.Fatalf("expected heal guidance, got %q", out.String())
	}

	out.Reset()
	if err := Doctor(p, DoctorOptions{Heal: true}, &out); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(*written, current) {
		t.Fatal("expected the live home's key to be written to the OS keyring")
	}
	if !strings.Contains(out.String(), "Keyring healed") {
		t.Fatalf("expected heal confirmation, got %q", out.String())
	}
}

func TestDoctorHealRefusesWhenNoEntryDecrypts(t *testing.T) {
	p := NewPaths(t.TempDir())
	stale := bytes.Repeat([]byte{1}, droidAuthKeySize)
	current := bytes.Repeat([]byte{7}, droidAuthKeySize)
	writeLiveEncryptedKeyringAuth(t, p.FactoryHome, droidCredentials{
		AccessToken:  "live-access",
		RefreshToken: "live-refresh",
	}, current)
	stubSystemKeyringEntries(t, [][]byte{stale})

	var out bytes.Buffer
	err := Doctor(p, DoctorOptions{Heal: true}, &out)
	if err == nil || !strings.Contains(err.Error(), "no OS keyring entry decrypts") {
		t.Fatalf("expected no-decrypting-entry error, got %v", err)
	}
	if !strings.Contains(out.String(), "Recovery:") {
		t.Fatalf("expected recovery guidance, got %q", out.String())
	}
}

func TestDoctorKeyfileFormatSkipsKeyring(t *testing.T) {
	p := NewPaths(t.TempDir())
	writeAuth(t, p.FactoryHome, "live-file", "live-key")

	var out bytes.Buffer
	if err := Doctor(p, DoctorOptions{Heal: true}, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "nothing to diagnose") {
		t.Fatalf("expected keyfile skip, got %q", out.String())
	}
}

func TestQuotaLoadsKeyringFormatAccount(t *testing.T) {
	p := NewPaths(t.TempDir())
	writeEncryptedKeyringAuth(t, p.AccountFactoryHome("work"), droidCredentials{
		AccessToken:  testJWT(time.Now().Add(time.Hour)),
		RefreshToken: "refresh-token",
	})
	withQuotaServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/billing/limits" {
			t.Fatalf("path = %q, want /api/billing/limits", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"usesTokenRateLimitsBilling":true,"limits":{"standard":{"fiveHour":{"usedPercent":5},"weekly":{"usedPercent":6},"monthly":{"usedPercent":7}}}}`))
	}))

	var out bytes.Buffer
	if err := Quota(p, QuotaOptions{Account: "work"}, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "5% used") {
		t.Fatalf("expected quota output for keyring account, got %q", out.String())
	}
}

func TestQuotaLoadsLoginKeychainFormatAccount(t *testing.T) {
	p := NewPaths(t.TempDir())
	key := bytes.Repeat([]byte{6}, droidAuthKeySize)
	writeLoginKeychainAuth(t, p.AccountFactoryHome("work"), droidCredentials{
		AccessToken:  testJWT(time.Now().Add(time.Hour)),
		RefreshToken: "refresh-token",
	}, key)
	withQuotaServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/billing/limits" {
			t.Fatalf("path = %q, want /api/billing/limits", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"usesTokenRateLimitsBilling":true,"limits":{"standard":{"fiveHour":{"usedPercent":8},"weekly":{"usedPercent":9},"monthly":{"usedPercent":10}}}}`))
	}))

	var out bytes.Buffer
	if err := Quota(p, QuotaOptions{Account: "work"}, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "8% used") {
		t.Fatalf("expected quota output for Login Keychain account, got %q", out.String())
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
	cli := CLI{Paths: p, Stdin: strings.NewReader("12\n"), Stdout: &out, Stderr: &errOut}
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

func writeEncryptedAuth(t *testing.T, factoryHome string, creds droidCredentials) {
	t.Helper()
	if err := os.MkdirAll(factoryHome, 0o700); err != nil {
		t.Fatal(err)
	}
	key := bytes.Repeat([]byte{7}, droidAuthKeySize)
	encrypted, err := encryptDroidCredentials(creds, key)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(factoryHome, authFileName), []byte(encrypted), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(factoryHome, authKeyFileName), []byte(base64.StdEncoding.EncodeToString(key)), 0o600); err != nil {
		t.Fatal(err)
	}
}

// writeKeyringAuth creates a saved-account home in keyring-v2 format: the
// Droid credentials file plus the switcher-owned key snapshot.
func writeKeyringAuth(t *testing.T, factoryHome, content string, key []byte) {
	t.Helper()
	writeLiveKeyringAuth(t, factoryHome, content)
	if err := os.WriteFile(filepath.Join(factoryHome, authKeyringKeyFileName), []byte(base64.StdEncoding.EncodeToString(key)), 0o600); err != nil {
		t.Fatal(err)
	}
}

// writeLiveKeyringAuth creates only the Droid-owned keyring file, matching a
// real Factory home where the key stays in the OS keyring.
func writeLiveKeyringAuth(t *testing.T, factoryHome, content string) {
	t.Helper()
	if err := os.MkdirAll(factoryHome, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(factoryHome, authKeyringFileName), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func writeEncryptedKeyringAuth(t *testing.T, factoryHome string, creds droidCredentials) {
	t.Helper()
	key := bytes.Repeat([]byte{9}, droidAuthKeySize)
	encrypted, err := encryptDroidCredentials(creds, key)
	if err != nil {
		t.Fatal(err)
	}
	writeKeyringAuth(t, factoryHome, encrypted, key)
}

// writeLiveEncryptedKeyringAuth creates only the Droid-owned keyring file with
// really encrypted credentials, matching a live Factory home during login.
func writeLiveEncryptedKeyringAuth(t *testing.T, factoryHome string, creds droidCredentials, key []byte) {
	t.Helper()
	encrypted, err := encryptDroidCredentials(creds, key)
	if err != nil {
		t.Fatal(err)
	}
	writeLiveKeyringAuth(t, factoryHome, encrypted)
}

func writeLiveEncryptedLoginKeychainAuth(t *testing.T, factoryHome string, creds droidCredentials, key []byte) {
	t.Helper()
	encrypted, err := encryptDroidCredentials(creds, key)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(factoryHome, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(factoryHome, authLoginKeychainFileName), []byte(encrypted), 0o600); err != nil {
		t.Fatal(err)
	}
}

func writeLoginKeychainAuth(t *testing.T, factoryHome string, creds droidCredentials, key []byte) {
	t.Helper()
	writeLiveEncryptedLoginKeychainAuth(t, factoryHome, creds, key)
	if err := os.WriteFile(filepath.Join(factoryHome, authLoginKeychainKeyFileName), []byte(base64.StdEncoding.EncodeToString(key)), 0o600); err != nil {
		t.Fatal(err)
	}
}

// assertKeyringAuthDecrypts checks the home's keyring file decrypts with key
// and carries the expected access token.
func assertKeyringAuthDecrypts(t *testing.T, factoryHome string, key []byte, wantAccessToken string) {
	t.Helper()
	assertSecureAuthDecrypts(t, factoryHome, authFormatKeyring, key, wantAccessToken)
}

func assertSecureAuthDecrypts(t *testing.T, factoryHome string, format authFormat, key []byte, wantAccessToken string) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(factoryHome, authCredentialsFileName(format))) //nolint:gosec // Test fixture path built from t.TempDir().
	if err != nil {
		t.Fatal(err)
	}
	plain, err := decryptDroidCredentials(strings.TrimSpace(string(raw)), key)
	if err != nil {
		t.Fatalf("keyring auth does not decrypt: %v", err)
	}
	var creds droidCredentials
	if err := json.Unmarshal(plain, &creds); err != nil {
		t.Fatal(err)
	}
	if creds.AccessToken != wantAccessToken {
		t.Fatalf("access token = %q, want %q", creds.AccessToken, wantAccessToken)
	}
}

// stubSystemKeyring replaces the OS secret store with an in-memory copy and
// returns a pointer to whatever was last written to it.
func stubSystemKeyring(t *testing.T, stored []byte) *[]byte {
	t.Helper()
	var written []byte
	oldRead, oldWrite := readSystemKeyringKey, writeSystemKeyringKey
	oldReadAll, oldClear := readAllSystemKeyringKeys, clearSystemKeyringKeys
	readSystemKeyringKey = func(authFormat) ([]byte, error) {
		if stored == nil {
			return nil, errors.New("OS keyring has no Droid key")
		}
		return stored, nil
	}
	readAllSystemKeyringKeys = func(authFormat) ([][]byte, error) {
		if stored == nil {
			return nil, errors.New("OS keyring has no Droid key")
		}
		return [][]byte{stored}, nil
	}
	clearSystemKeyringKeys = func(authFormat) error {
		stored = nil
		return nil
	}
	writeSystemKeyringKey = func(_ authFormat, key []byte) error {
		written = key
		stored = key
		return nil
	}
	t.Cleanup(func() {
		readSystemKeyringKey = oldRead
		writeSystemKeyringKey = oldWrite
		readAllSystemKeyringKeys = oldReadAll
		clearSystemKeyringKeys = oldClear
	})
	return &written
}

// stubSystemKeyringEntries replaces the OS secret store with an in-memory
// multi-entry copy, mimicking a backend that keeps duplicate entries. Plain
// lookups answer with the first entry. It returns a pointer to whatever was
// last written to it.
func stubSystemKeyringEntries(t *testing.T, entries [][]byte) *[]byte {
	t.Helper()
	var written []byte
	oldRead, oldWrite := readSystemKeyringKey, writeSystemKeyringKey
	oldReadAll, oldClear := readAllSystemKeyringKeys, clearSystemKeyringKeys
	readSystemKeyringKey = func(authFormat) ([]byte, error) {
		if len(entries) == 0 {
			return nil, errors.New("OS keyring has no Droid key")
		}
		return entries[0], nil
	}
	readAllSystemKeyringKeys = func(authFormat) ([][]byte, error) {
		if len(entries) == 0 {
			return nil, errors.New("OS keyring has no Droid key")
		}
		return entries, nil
	}
	clearSystemKeyringKeys = func(authFormat) error {
		entries = nil
		return nil
	}
	writeSystemKeyringKey = func(_ authFormat, key []byte) error {
		entries = [][]byte{key}
		written = key
		return nil
	}
	t.Cleanup(func() {
		readSystemKeyringKey = oldRead
		writeSystemKeyringKey = oldWrite
		readAllSystemKeyringKeys = oldReadAll
		clearSystemKeyringKeys = oldClear
	})
	return &written
}

func testJWT(exp time.Time) string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none"}`))
	payload, err := json.Marshal(map[string]any{
		"exp":   exp.Unix(),
		"sub":   "user-1",
		"email": "user@example.com",
	})
	if err != nil {
		panic(err)
	}
	return header + "." + base64.RawURLEncoding.EncodeToString(payload) + ".sig"
}

func withQuotaServer(t *testing.T, handler http.Handler) {
	t.Helper()
	server := httptest.NewServer(handler)
	oldClient := quotaHTTPClient
	oldFactory := factoryAPIBaseURL
	oldWorkOS := workOSBaseURL
	quotaHTTPClient = server.Client()
	factoryAPIBaseURL = server.URL
	workOSBaseURL = server.URL
	t.Cleanup(func() {
		quotaHTTPClient = oldClient
		factoryAPIBaseURL = oldFactory
		workOSBaseURL = oldWorkOS
		server.Close()
	})
}

func assertFile(t *testing.T, path, want string) {
	t.Helper()
	got, err := os.ReadFile(path) //nolint:gosec // Test paths are created inside t.TempDir.
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Fatalf("%s = %q, want %q", path, string(got), want)
	}
}

func writeTestSession(t *testing.T, path, id, title, orgID, cwd string, extraLines ...string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	start := map[string]any{
		"type":  "session_start",
		"id":    id,
		"title": title,
		"cwd":   cwd,
	}
	if orgID != "" {
		start["organizationId"] = orgID
	}
	data, err := json.Marshal(start)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data) + "\n"
	for _, l := range extraLines {
		content += l + "\n"
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestFindAndShareSessions(t *testing.T) {
	p := NewPaths(t.TempDir())
	sessDir := p.FactorySessions()

	s1 := filepath.Join(sessDir, "-project-a", "s1.jsonl")
	s2 := filepath.Join(sessDir, "-project-b", "s2.jsonl")
	s3 := filepath.Join(sessDir, "s3.jsonl")

	writeTestSession(t, s1, "s1", "Session 1", "org-a", "/project-a", `{"type":"message","text":"hello"}`)
	writeTestSession(t, s2, "s2", "Session 2", "", "/project-b")
	writeTestSession(t, s3, "s3", "Session 3", "org-b", "/project-c")

	found, err := FindSessions(sessDir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 3 {
		t.Fatalf("expected 3 sessions, got %d", len(found))
	}

	// Filter by session ID
	filtered, err := FindSessions(sessDir, []string{"s1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(filtered) != 1 || filtered[0].ID != "s1" {
		t.Fatalf("expected filtered session s1, got %+v", filtered)
	}

	// Dry run should not modify files
	var dryOut bytes.Buffer
	if err := ShareSessions(p, ShareOptions{DryRun: true}, &dryOut); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(dryOut.String(), "Dry run: 2 session(s) would be updated") {
		t.Fatalf("unexpected dry run output: %s", dryOut.String())
	}
	// Check s1 still has org-a
	s1Info, _ := readSessionSummary(s1)
	if s1Info.OrganizationID != "org-a" {
		t.Fatalf("expected s1 to still have org-a, got %q", s1Info.OrganizationID)
	}

	// Real share: should unbind s1 and s3
	var shareOut bytes.Buffer
	if err := ShareSessions(p, ShareOptions{}, &shareOut); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(shareOut.String(), "Shared 2 session(s) across accounts") {
		t.Fatalf("unexpected share output: %s", shareOut.String())
	}

	s1Info, _ = readSessionSummary(s1)
	if s1Info.OrganizationID != "" {
		t.Fatalf("expected s1 org to be cleared, got %q", s1Info.OrganizationID)
	}
	// Verify second line was preserved
	content, err := os.ReadFile(s1) //nolint:gosec // Test paths are created inside t.TempDir.
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), `{"type":"message","text":"hello"}`) {
		t.Fatalf("expected preserved message content in s1, got:\n%s", string(content))
	}

	// Running share again should report already shared
	shareOut.Reset()
	if err := ShareSessions(p, ShareOptions{}, &shareOut); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(shareOut.String(), "All sessions are already shared across accounts") {
		t.Fatalf("unexpected already-shared output: %s", shareOut.String())
	}
}

func TestShareSessionsAdopt(t *testing.T) {
	p := NewPaths(t.TempDir())
	sessDir := p.FactorySessions()
	s1 := filepath.Join(sessDir, "s1.jsonl")
	writeTestSession(t, s1, "s1", "Session 1", "org-old", "/project")

	var out bytes.Buffer
	if err := ShareSessions(p, ShareOptions{AdoptOrg: "org-new"}, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Adopted 1 session(s) into organization org-new") {
		t.Fatalf("unexpected adopt output: %s", out.String())
	}
	info, _ := readSessionSummary(s1)
	if info.OrganizationID != "org-new" {
		t.Fatalf("expected org-new, got %q", info.OrganizationID)
	}
}

func TestSwitchAccountShareSessionsAndNotice(t *testing.T) {
	p := NewPaths(t.TempDir())
	writeEncryptedAuth(t, p.AccountFactoryHome("bizkit"), droidCredentials{
		AccessToken:          "bizkit-access",
		RefreshToken:         "bizkit-refresh",
		ActiveOrganizationID: "org-bizkit",
	})
	writeEncryptedAuth(t, p.FactoryHome, droidCredentials{
		AccessToken:          "involens-access",
		RefreshToken:         "involens-refresh",
		ActiveOrganizationID: "org-involens",
	})

	s1 := filepath.Join(p.FactorySessions(), "s1.jsonl")
	writeTestSession(t, s1, "s1", "Session Involens", "org-involens", "/devel")

	// Switch without share-sessions should print notice
	var out bytes.Buffer
	if err := SwitchAccount(p, "bizkit", &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Notice: 1 session(s) belong to organization \"org-involens\"") {
		t.Fatalf("expected notice about hidden sessions, got:\n%s", out.String())
	}

	// Switch back to involens and then switch with ShareSessions
	writeEncryptedAuth(t, p.AccountFactoryHome("involens"), droidCredentials{
		AccessToken:          "involens-access",
		RefreshToken:         "involens-refresh",
		ActiveOrganizationID: "org-involens",
	})
	out.Reset()
	if err := SwitchAccountWithOptions(p, "involens", SwitchOptions{ShareSessions: true}, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Shared 1 session(s) across accounts") {
		t.Fatalf("expected shared sessions message, got:\n%s", out.String())
	}

	info, _ := readSessionSummary(s1)
	if info.OrganizationID != "" {
		t.Fatalf("expected session to be unbound, got %q", info.OrganizationID)
	}
}

func TestCLIShareSessions(t *testing.T) {
	p := NewPaths(t.TempDir())
	s1 := filepath.Join(p.FactorySessions(), "s1.jsonl")
	writeTestSession(t, s1, "s1", "Session 1", "org-x", "/dir")

	var out bytes.Buffer
	var errOut bytes.Buffer
	cli := CLI{Paths: p, Stdout: &out, Stderr: &errOut}

	// Test list
	if err := cli.Run([]string{"droid-switcher", "share-sessions", "--list"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "SESSION ID") || !strings.Contains(out.String(), "org-x") {
		t.Fatalf("unexpected list output: %s", out.String())
	}

	// Test run
	out.Reset()
	if err := cli.Run([]string{"droid-switcher", "share-sessions"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Shared 1 session(s) across accounts") {
		t.Fatalf("unexpected run output: %s", out.String())
	}
}
