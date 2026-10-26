package main

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// isolateOmniAccounts points the pool at a throwaway file so a developer's real
// accounts never leak into a test run.
func isolateOmniAccounts(t *testing.T) {
	t.Helper()
	t.Setenv("OMNIRUSH_ACCOUNTS_FILE", filepath.Join(t.TempDir(), "accounts.json"))
	resetOmniAuthCache()
}

func TestOmniRushAuthRoundTrip(t *testing.T) {
	isolateOmniAccounts(t)
	tmpDir := t.TempDir()
	authPath := filepath.Join(tmpDir, "auth.json")
	t.Setenv("OMNIRUSH_AUTH_FILE", authPath)
	omniAuthMu.Lock()
	cachedOmniAuth = nil
	lastOmniAuthCheck = time.Time{}
	omniAuthMu.Unlock()

	// Initial load should fail when file does not exist
	_, _, err := loadOmniRushAuth()
	if err == nil {
		t.Fatal("expected error when auth file does not exist")
	}

	auth := &OmniRushAuth{
		AccessToken:  "test-access-token-12345",
		RefreshToken: "test-refresh-token-67890",
		GatewayURL:   "https://omnirush.ai/omnirush",
		Email:        "user@example.com",
	}

	if err := saveOmniRushAuth(auth); err != nil {
		t.Fatalf("saveOmniRushAuth error: %v", err)
	}

	loaded, path, err := loadOmniRushAuth()
	if err != nil {
		t.Fatalf("loadOmniRushAuth error: %v", err)
	}
	if path != authPath {
		t.Errorf("expected path %s, got %s", authPath, path)
	}
	if loaded.AccessToken != auth.AccessToken {
		t.Errorf("expected access token %s, got %s", auth.AccessToken, loaded.AccessToken)
	}
	if loaded.RefreshToken != auth.RefreshToken {
		t.Errorf("expected refresh token %s, got %s", auth.RefreshToken, loaded.RefreshToken)
	}
	if loaded.Email != auth.Email {
		t.Errorf("expected email %s, got %s", auth.Email, loaded.Email)
	}

	// Test clear
	if err := clearOmniRushAuth(); err != nil {
		t.Fatalf("clearOmniRushAuth error: %v", err)
	}
	_, _, errAfter := loadOmniRushAuth()
	if errAfter == nil {
		t.Fatal("expected error after clearOmniRushAuth")
	}
}

func TestCallOmniRushUnauthenticatedReturnsError(t *testing.T) {
	isolateOmniAccounts(t)
	tmpDir := t.TempDir()
	t.Setenv("OMNIRUSH_AUTH_FILE", filepath.Join(tmpDir, "nonexistent.json"))
	t.Setenv("OMNIRUSH_ACCESS_TOKEN", "")
	t.Setenv("OMNIRUSH_REFRESH_TOKEN", "")
	omniAuthMu.Lock()
	cachedOmniAuth = nil
	omniAuthMu.Unlock()

	_, err := callOmniRush("gpt-6-astra", []byte(`{"messages":[{"role":"user","content":"hi"}]}`), false)
	if err == nil {
		t.Fatal("expected error when calling callOmniRush without credentials")
	}
	if !strings.Contains(err.Error(), "authentication required") {
		t.Errorf("expected error to mention authentication required, got: %v", err)
	}
}

func TestOmniRushModelsAreInListModels(t *testing.T) {
	// No credentials in the test environment: the catalog falls back to the
	// known-good ids, which must still be advertised.
	omniCatalogMu.Lock()
	omniCatalogCache = nil
	omniCatalogFetched = time.Time{}
	omniCatalogMu.Unlock()

	models := listModels()
	found := make(map[string]bool)
	for _, m := range models {
		if id, ok := m["ID"].(string); ok {
			found[id] = true
		}
	}

	for _, spec := range omniRushFallbackModels {
		if !found[spec.ID] {
			t.Errorf("expected omnirush model %s to be listed in listModels()", spec.ID)
		}
	}
}
