package main

import (
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"
)

func newTestStore(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	// Isolate every credential location, otherwise the developer's real session
	// gets migrated into the test pool.
	t.Setenv("HOME", home)
	t.Setenv("OMNIRUSH_DIR", "")
	t.Setenv("OMNIRUSH_AUTH_FILE", filepath.Join(home, "legacy-auth.json"))
	t.Setenv("OMNIRUSH_ACCESS_TOKEN", "")
	t.Setenv("OMNIRUSH_REFRESH_TOKEN", "")
	path := filepath.Join(home, "accounts.json")
	t.Setenv("OMNIRUSH_ACCOUNTS_FILE", path)
	resetOmniAuthCache()
	return path
}

func TestOmniAccountsMigrateLegacyFile(t *testing.T) {
	newTestStore(t)
	legacy := filepath.Join(t.TempDir(), "auth.json")
	t.Setenv("OMNIRUSH_AUTH_FILE", legacy)
	t.Setenv("OMNIRUSH_DIR", "")
	if err := os.WriteFile(legacy, []byte(`{"accessToken":"a1","refreshToken":"r1","gatewayUrl":"https://omnirush.ai/omnirush/v1","email":"one@example.com"}`), 0o600); err != nil {
		t.Fatalf("write legacy: %v", err)
	}

	store, err := loadOmniAccounts()
	if err != nil {
		t.Fatalf("loadOmniAccounts: %v", err)
	}
	if len(store.Accounts) != 1 {
		t.Fatalf("expected 1 migrated account, got %d", len(store.Accounts))
	}
	acct := store.Accounts[0]
	if acct.Email != "one@example.com" || acct.AccessToken != "a1" {
		t.Errorf("migrated account = %+v", acct)
	}
	if acct.LegacyPath != legacy {
		t.Errorf("legacy path = %q, want %q", acct.LegacyPath, legacy)
	}
	if !acct.Enabled {
		t.Error("a migrated account should start enabled")
	}
}

func TestUpsertOmniAccountDeduplicatesByEmail(t *testing.T) {
	path := newTestStore(t)

	if _, err := upsertOmniAccount(&OmniRushAuth{Email: "a@example.com", AccessToken: "old", RefreshToken: "r1"}, ""); err != nil {
		t.Fatalf("first upsert: %v", err)
	}
	if _, err := upsertOmniAccount(&OmniRushAuth{Email: "A@Example.com", AccessToken: "new", RefreshToken: "r2"}, ""); err != nil {
		t.Fatalf("second upsert: %v", err)
	}
	if _, err := upsertOmniAccount(&OmniRushAuth{Email: "b@example.com", AccessToken: "b1", RefreshToken: "rb"}, ""); err != nil {
		t.Fatalf("third upsert: %v", err)
	}

	store, err := loadOmniAccounts()
	if err != nil {
		t.Fatalf("loadOmniAccounts: %v", err)
	}
	if len(store.Accounts) != 2 {
		t.Fatalf("expected 2 accounts, got %d", len(store.Accounts))
	}
	for _, acct := range store.Accounts {
		if acct.Email == "a@example.com" && acct.AccessToken != "new" {
			t.Errorf("re-login should refresh the token, got %q", acct.AccessToken)
		}
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat store: %v", err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Errorf("store mode = %o, want 600", info.Mode().Perm())
	}
}

func TestSelectOmniAccountPrefersLeastRecentlyUsed(t *testing.T) {
	newTestStore(t)
	if _, err := upsertOmniAccount(&OmniRushAuth{Email: "old@example.com", AccessToken: "t1", RefreshToken: "r1"}, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := upsertOmniAccount(&OmniRushAuth{Email: "new@example.com", AccessToken: "t2", RefreshToken: "r2"}, ""); err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC()
	if err := updateOmniAccount("old@example.com", func(a *OmniRushAccount) {
		a.LastUsed = now.Add(-time.Hour).Format(time.RFC3339)
	}); err != nil {
		t.Fatal(err)
	}
	if err := updateOmniAccount("new@example.com", func(a *OmniRushAccount) {
		a.LastUsed = now.Format(time.RFC3339)
	}); err != nil {
		t.Fatal(err)
	}

	acct, err := selectOmniAccount(nil)
	if err != nil {
		t.Fatalf("selectOmniAccount: %v", err)
	}
	if acct.Email != "old@example.com" {
		t.Errorf("expected the idle account first, got %q", acct.Email)
	}

	// An excluded account must not be handed back again for the same request.
	acct, err = selectOmniAccount(map[string]bool{"old@example.com": true})
	if err != nil {
		t.Fatalf("selectOmniAccount with exclusion: %v", err)
	}
	if acct.Email != "new@example.com" {
		t.Errorf("expected the other account, got %q", acct.Email)
	}
}

func TestSelectOmniAccountSkipsDisabledAndCooling(t *testing.T) {
	newTestStore(t)
	if _, err := upsertOmniAccount(&OmniRushAuth{Email: "a@example.com", AccessToken: "t1", RefreshToken: "r1"}, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := upsertOmniAccount(&OmniRushAuth{Email: "b@example.com", AccessToken: "t2", RefreshToken: "r2"}, ""); err != nil {
		t.Fatal(err)
	}

	if err := setOmniAccountEnabled("a@example.com", false); err != nil {
		t.Fatal(err)
	}
	penalizeOmniAccount("b@example.com", "throttled", time.Minute)

	// Both are unusable: the fallback keeps a single-account style call working
	// rather than hard-failing the request.
	acct, err := selectOmniAccount(nil)
	if err != nil {
		t.Fatalf("expected a fallback account, got error %v", err)
	}
	if acct == nil || acct.Email == "" {
		t.Error("fallback should still return a concrete account")
	}

	if err := setOmniAccountEnabled("a@example.com", true); err != nil {
		t.Fatal(err)
	}
	acct, err = selectOmniAccount(nil)
	if err != nil {
		t.Fatalf("select after re-enable: %v", err)
	}
	if acct.Email != "a@example.com" {
		t.Errorf("expected the enabled account, got %q", acct.Email)
	}

	touchOmniAccount("b@example.com")
	fresh, err := findOmniAccount("b@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if fresh.status() != "active" {
		t.Errorf("touch should clear the error state, got %q (%s)", fresh.status(), fresh.LastError)
	}
}

func TestRemoveOmniAccountDropsUnusedLegacyMirror(t *testing.T) {
	newTestStore(t)
	legacy := filepath.Join(t.TempDir(), "auth.json")
	if err := os.WriteFile(legacy, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := upsertOmniAccount(&OmniRushAuth{Email: "a@example.com", AccessToken: "t1", RefreshToken: "r1"}, legacy); err != nil {
		t.Fatal(err)
	}
	if err := removeOmniAccount("a@example.com"); err != nil {
		t.Fatalf("removeOmniAccount: %v", err)
	}

	store, err := loadOmniAccounts()
	if err != nil {
		t.Fatal(err)
	}
	if len(store.Accounts) != 0 {
		t.Errorf("expected an empty pool, got %d", len(store.Accounts))
	}
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Error("the CLI mirror should be removed once no account points at it")
	}
}

// The pool is read on every request and written by rotations, so concurrent
// selection and updates must be race-free (run with -race).
func TestOmniAccountPoolConcurrentRotation(t *testing.T) {
	newTestStore(t)
	for _, email := range []string{"a@example.com", "b@example.com", "c@example.com"} {
		if _, err := upsertOmniAccount(&OmniRushAuth{Email: email, AccessToken: "t-" + email, RefreshToken: "r-" + email}, ""); err != nil {
			t.Fatalf("upsert %s: %v", email, err)
		}
	}

	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			acct, err := selectOmniAccount(nil)
			if err != nil || acct == nil {
				return
			}
			touchOmniAccount(acct.ID)
			_ = omniAccountsForUI()
			if n%3 == 0 {
				penalizeOmniAccount(acct.ID, "test", 10*time.Millisecond)
			}
		}(i)
	}
	wg.Wait()

	store, err := loadOmniAccounts()
	if err != nil {
		t.Fatalf("loadOmniAccounts: %v", err)
	}
	if len(store.Accounts) != 3 {
		t.Errorf("expected 3 accounts after concurrent use, got %d", len(store.Accounts))
	}
}
