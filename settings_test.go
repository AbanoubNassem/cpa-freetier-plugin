package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func resetSettingsCache() {
	settingsMu.Lock()
	currentConfig = nil
	settingsStamp = ""
	settingsMu.Unlock()
}

func TestSettingsDefaultsAreUsable(t *testing.T) {
	resetSettingsCache()
	t.Setenv("CPA_FREETIER_SETTINGS", filepath.Join(t.TempDir(), "settings.json"))

	cfg := loadPluginSettings()
	if !cfg.Identity.MimicCLI {
		t.Error("the CLI disguise should be on by default")
	}
	if cfg.Identity.UserAgent != opencodeClientUA {
		t.Errorf("default user agent = %q, want %q", cfg.Identity.UserAgent, opencodeClientUA)
	}
	if cfg.Request.HeaderTimeoutSec != defaultHeaderTimeout {
		t.Errorf("default header timeout = %d, want %d", cfg.Request.HeaderTimeoutSec, defaultHeaderTimeout)
	}
	if cfg.Identity.Sidecar != "" {
		t.Errorf("sidecar should default to auto (empty), got %q", cfg.Identity.Sidecar)
	}
}

func TestSettingsSaveLoadRoundTrip(t *testing.T) {
	resetSettingsCache()
	path := filepath.Join(t.TempDir(), "settings.json")
	t.Setenv("CPA_FREETIER_SETTINGS", path)

	cfg := defaultPluginSettings()
	cfg.Proxy.OpenCode = "127.0.0.1:8127"
	cfg.Identity.UserAgent = "opencode/latest/9.9.9/cli"
	cfg.Request.HeaderTimeoutSec = 200
	if err := savePluginSettings(cfg); err != nil {
		t.Fatalf("savePluginSettings: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("settings file missing: %v", err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Errorf("settings file mode = %o, want 600", info.Mode().Perm())
	}

	resetSettingsCache()
	loaded := loadPluginSettings()
	if loaded.Proxy.OpenCode != "127.0.0.1:8127" {
		t.Errorf("opencode proxy = %q after reload", loaded.Proxy.OpenCode)
	}
	if loaded.Request.HeaderTimeoutSec != 200 {
		t.Errorf("header timeout = %d after reload", loaded.Request.HeaderTimeoutSec)
	}
	if loaded.Identity.UserAgent != "opencode/latest/9.9.9/cli" {
		t.Errorf("user agent = %q after reload", loaded.Identity.UserAgent)
	}
}

// A typo in a proxy address must be rejected at save time; otherwise the failure
// only shows up later as an opaque upstream timeout.
func TestSettingsRejectsUnusableProxy(t *testing.T) {
	resetSettingsCache()
	t.Setenv("CPA_FREETIER_SETTINGS", filepath.Join(t.TempDir(), "settings.json"))

	cases := []string{"::::", "http://", "127.0.0.1:99999"}
	for _, bad := range cases {
		cfg := defaultPluginSettings()
		cfg.Proxy.OpenCode = bad
		if err := savePluginSettings(cfg); err == nil {
			t.Errorf("expected %q to be rejected", bad)
		}
	}

	cfg := defaultPluginSettings()
	cfg.Proxy.OpenCodePool = "127.0.0.1:8127,127.0.0.1:8128"
	if err := savePluginSettings(cfg); err != nil {
		t.Errorf("a valid pool should be accepted: %v", err)
	}
}

// "-" disables egress/identity features explicitly: usable for the pool, for a
// single proxy, and for the sidecar.
func TestSettingsDisabledSentinel(t *testing.T) {
	resetSettingsCache()
	t.Setenv("CPA_FREETIER_SETTINGS", filepath.Join(t.TempDir(), "settings.json"))

	cfg := defaultPluginSettings()
	cfg.Proxy.OpenCode = proxyDisabledSentinel
	cfg.Proxy.OpenCodePool = proxyDisabledSentinel
	cfg.Identity.Sidecar = proxyDisabledSentinel
	if err := savePluginSettings(cfg); err != nil {
		t.Fatalf("sentinel values should be valid: %v", err)
	}

	if u, handled := settingsOpenCodeProxy(); !handled || u != nil {
		t.Error("a '-' opencode proxy should resolve to direct with handled=true")
	}
	if pool := proxyPool("OPENCODE_PROXY_POOL", defaultOpenCodeProxies); pool != nil {
		t.Errorf("a '-' pool should resolve to nothing, got %v", pool)
	}
	if base, handled := settingsSidecarBase(); !handled || base != "" {
		t.Errorf("a '-' sidecar should resolve to empty+handled, got %q/%v", base, handled)
	}
}

// The file wins over the environment so the dashboard is authoritative.
func TestSettingsOverrideEnvironment(t *testing.T) {
	resetSettingsCache()
	t.Setenv("CPA_FREETIER_SETTINGS", filepath.Join(t.TempDir(), "settings.json"))
	t.Setenv("OPENCODE_PROXY", "127.0.0.1:9999")

	cfg := defaultPluginSettings()
	cfg.Proxy.OpenCode = "127.0.0.1:8127"
	if err := savePluginSettings(cfg); err != nil {
		t.Fatalf("savePluginSettings: %v", err)
	}

	u := getOpenCodeProxy()
	if u == nil || u.String() != "http://127.0.0.1:8127" {
		t.Fatalf("settings proxy should win over env, got %v", u)
	}
}

func TestSettingsHeaderTimeoutFallback(t *testing.T) {
	resetSettingsCache()
	t.Setenv("CPA_FREETIER_SETTINGS", filepath.Join(t.TempDir(), "settings.json"))

	// A save with no timeout is a UI bug and must be rejected...
	cfg := defaultPluginSettings()
	cfg.Request.HeaderTimeoutSec = 0
	if err := savePluginSettings(cfg); err == nil {
		t.Error("a zero header timeout should be rejected on save")
	}

	// ...while an older file that simply lacks the field still reads as default.
	raw := `{"request":{"maxRetries":4}}`
	if err := os.WriteFile(filepath.Join(filepath.Dir(settingsPath()), "old.json"), []byte(raw), 0o600); err != nil {
		t.Skipf("cannot stage legacy file: %v", err)
	}
	cfg = defaultPluginSettings()
	cfg.Request.HeaderTimeoutSec = 0
	normalizeSettings(cfg)
	if got := cfg.Request.HeaderTimeoutSec; got != defaultHeaderTimeout {
		t.Errorf("zero timeout should normalise to %d, got %d", defaultHeaderTimeout, got)
	}
}

// A queued free-tier generation must fail with a readable message instead of
// waiting for the generic header timeout.
func TestOmniRushTimeoutAndReasoningSettings(t *testing.T) {
	resetSettingsCache()
	t.Setenv("CPA_FREETIER_SETTINGS", filepath.Join(t.TempDir(), "settings.json"))

	if got := settingsOmniRushTimeout(); got.Seconds() != float64(defaultOmniRushTimeout) {
		t.Errorf("default omnirush timeout = %v, want %ds", got, defaultOmniRushTimeout)
	}

	cfg := defaultPluginSettings()
	cfg.Request.OmniRushTimeoutSec = 90
	cfg.Request.OmniRushReasoning = "Low"
	if err := savePluginSettings(cfg); err != nil {
		t.Fatalf("savePluginSettings: %v", err)
	}
	if got := settingsOmniRushTimeout(); got.Seconds() != 90 {
		t.Errorf("omnirush timeout = %v, want 90s", got)
	}
	if got := settingsOmniRushReasoning(); got != "low" {
		t.Errorf("reasoning = %q, want low (normalised)", got)
	}

	cfg.Request.OmniRushReasoning = "turbo"
	if err := savePluginSettings(cfg); err == nil {
		t.Error("an unknown reasoning level should be rejected")
	}
	cfg.Request.OmniRushReasoning = ""
	cfg.Request.OmniRushTimeoutSec = 1
	if err := savePluginSettings(cfg); err == nil {
		t.Error("a sub-minimum timeout should be rejected")
	}
}

// The dashboard default only fills the gap; an explicit client value wins.
func TestOmniRushReasoningFallback(t *testing.T) {
	resetSettingsCache()
	t.Setenv("CPA_FREETIER_SETTINGS", filepath.Join(t.TempDir(), "settings.json"))

	cfg := defaultPluginSettings()
	cfg.Request.OmniRushReasoning = "low"
	if err := savePluginSettings(cfg); err != nil {
		t.Fatalf("savePluginSettings: %v", err)
	}

	if got := omniRushReasoningEffort(&chatCompletionRequest{}); got != "low" {
		t.Errorf("empty request should fall back to the setting, got %q", got)
	}
	if got := omniRushReasoningEffort(&chatCompletionRequest{ReasoningEffort: "high"}); got != "high" {
		t.Errorf("explicit request effort should win, got %q", got)
	}
	if got := omniRushReasoningEffort(&chatCompletionRequest{ReasoningEffort: "off"}); got != "" {
		t.Errorf("explicit off should stay off, got %q", got)
	}
}

func TestReferralCodeNormalisationAndURL(t *testing.T) {
	cases := []struct{ in, want string }{
		{"5WSC5CWX", "5WSC5CWX"},
		{" https://omnirush.ai/console?ref=5WSC5CWX ", "5WSC5CWX"},
		{"https://omnirush.ai/console?ref=ABC&x=1", "ABC"},
		{"", ""},
		{"-", ""},
	}
	for _, c := range cases {
		if got := normalizeReferralCode(c.in); got != c.want {
			t.Errorf("normalizeReferralCode(%q) = %q, want %q", c.in, got, c.want)
		}
	}

	resetSettingsCache()
	t.Setenv("CPA_FREETIER_SETTINGS", filepath.Join(t.TempDir(), "settings.json"))
	cfg := defaultPluginSettings()
	cfg.Identity.ReferralCode = "5WSC5CWX"
	if err := savePluginSettings(cfg); err != nil {
		t.Fatalf("savePluginSettings: %v", err)
	}

	got := withReferral("https://omnirush.ai/console?code=ABCD-1234")
	if !strings.Contains(got, "ref=5WSC5CWX") {
		t.Errorf("referral missing from %q", got)
	}
	if !strings.Contains(got, "code=ABCD-1234") {
		t.Errorf("device code must survive: %q", got)
	}

	// The disabled sentinel strips the parameter entirely.
	cfg.Identity.ReferralCode = "-"
	if err := savePluginSettings(cfg); err != nil {
		t.Fatalf("savePluginSettings: %v", err)
	}
	if got := withReferral("https://omnirush.ai/console?code=X"); strings.Contains(got, "ref=") {
		t.Errorf("referral should be disabled, got %q", got)
	}
}
