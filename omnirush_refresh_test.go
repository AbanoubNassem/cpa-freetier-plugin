package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// resetOmniAuthCache drops the process-wide auth cache so a test can seed its
// own auth file without inheriting the previous test's tokens.
func resetOmniAuthCache() {
	invalidateOmniStore()
}

func TestOmniRushDeviceOrigin(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"api base with v1", "https://omnirush.ai/omnirush/v1", "https://omnirush.ai/omnirush"},
		{"api base with trailing slash", "https://omnirush.ai/omnirush/v1/", "https://omnirush.ai/omnirush"},
		{"device root already", "https://omnirush.ai/omnirush", "https://omnirush.ai/omnirush"},
		{"device root trailing slash", "https://omnirush.ai/omnirush/", "https://omnirush.ai/omnirush"},
		{"bare host", "https://omnirush.ai", "https://omnirush.ai"},
		{"empty falls back", "", defaultOmniRushOrigin},
		{"spaces fall back", "   ", defaultOmniRushOrigin},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := omniRushDeviceOrigin(tc.in); got != tc.want {
				t.Fatalf("omniRushDeviceOrigin(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// The refresh call must hit /device/refresh on the host root, not on the API
// base. Pointing at /v1/device/refresh answers 401 device_token_required, which
// surfaced to users as "refresh token expired or invalid".
func TestRefreshOmniRushTokenUsesDeviceEndpoint(t *testing.T) {
	isolateOmniAccounts(t)
	var gotPath, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		gotBody = body["refresh_token"]
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"omnirush-new-access","refresh_token":"omr_new_refresh","gateway_url":"` + srvURLPlaceholder + `"}`))
	}))
	defer srv.Close()

	authFile := filepath.Join(t.TempDir(), "auth.json")
	seed := map[string]any{
		"accessToken":  "omnirush-stale-access",
		"refreshToken": "omr_seed_refresh",
		"gatewayUrl":   srv.URL + "/omnirush/v1",
	}
	data, _ := json.Marshal(seed)
	if err := os.WriteFile(authFile, data, 0o600); err != nil {
		t.Fatalf("seed auth file: %v", err)
	}
	t.Setenv("OMNIRUSH_AUTH_FILE", authFile)
	resetOmniAuthCache()

	auth, err := refreshOmniRushToken("omnirush-stale-access")
	if err != nil {
		t.Fatalf("refreshOmniRushToken: %v", err)
	}
	if gotPath != "/omnirush/device/refresh" {
		t.Fatalf("refresh hit %q, want /omnirush/device/refresh", gotPath)
	}
	if gotBody != "omr_seed_refresh" {
		t.Fatalf("refresh sent token %q, want seeded token", gotBody)
	}
	if auth.AccessToken != "omnirush-new-access" || auth.RefreshToken != "omr_new_refresh" {
		t.Fatalf("unexpected rotated pair: access=%q refresh=%q", auth.AccessToken, auth.RefreshToken)
	}

	// The rotated pair must land in the file it came from, so a co-installed CLI
	// cannot keep refreshing with a token that was already spent.
	saved, err := os.ReadFile(authFile)
	if err != nil {
		t.Fatalf("read auth file: %v", err)
	}
	if !strings.Contains(string(saved), "omnirush-new-access") || !strings.Contains(string(saved), "omr_new_refresh") {
		t.Fatalf("rotated tokens were not persisted to the source file: %s", saved)
	}
}

// A concurrent 401 storm must not burn refresh tokens: once one caller has
// rotated, the others reuse the fresh access token.
func TestRefreshOmniRushTokenReusesFreshPair(t *testing.T) {
	isolateOmniAccounts(t)
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"omnirush-fresh","refresh_token":"omr_fresh","gateway_url":"https://omnirush.ai/omnirush/v1"}`))
	}))
	defer srv.Close()

	authFile := filepath.Join(t.TempDir(), "auth.json")
	seed := map[string]any{
		"accessToken":  "omnirush-fresh", // already rotated by a peer
		"refreshToken": "omr_peer",
		"gatewayUrl":   srv.URL,
	}
	data, _ := json.Marshal(seed)
	if err := os.WriteFile(authFile, data, 0o600); err != nil {
		t.Fatalf("seed auth file: %v", err)
	}
	t.Setenv("OMNIRUSH_AUTH_FILE", authFile)
	resetOmniAuthCache()

	auth, err := refreshOmniRushToken("omnirush-stale")
	if err != nil {
		t.Fatalf("refreshOmniRushToken: %v", err)
	}
	if hits != 0 {
		t.Fatalf("expected no network refresh, got %d call(s)", hits)
	}
	if auth.AccessToken != "omnirush-fresh" {
		t.Fatalf("expected the peer's access token, got %q", auth.AccessToken)
	}
}

// Patch the placeholder inside the first test server's JSON response.
const srvURLPlaceholder = "https://omnirush.ai/omnirush/v1"

// A 409 from the gateway means another client already spent this refresh token;
// the plugin should adopt the pair that client stored rather than demanding a
// fresh sign-in.
func TestRefreshOmniRushTokenAdoptsPairAfterConflict(t *testing.T) {
	isolateOmniAccounts(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"detail":"refresh_token_already_used"}`))
	}))
	defer srv.Close()

	authFile := filepath.Join(t.TempDir(), "auth.json")
	seed := map[string]any{
		"accessToken":  "omnirush-from-cli",
		"refreshToken": "omr_from_cli",
		"gatewayUrl":   srv.URL + "/omnirush/v1",
	}
	data, _ := json.Marshal(seed)
	if err := os.WriteFile(authFile, data, 0o600); err != nil {
		t.Fatalf("seed auth file: %v", err)
	}
	t.Setenv("OMNIRUSH_AUTH_FILE", authFile)
	resetOmniAuthCache()

	auth, err := refreshOmniRushToken("omnirush-stale")
	if err != nil {
		t.Fatalf("expected the peer pair to be adopted, got error: %v", err)
	}
	if auth.AccessToken != "omnirush-from-cli" {
		t.Fatalf("expected the CLI's access token, got %q", auth.AccessToken)
	}
}
