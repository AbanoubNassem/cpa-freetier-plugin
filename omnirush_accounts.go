package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// OmniRushAccount is one signed-in OmniRush identity. The gateway hands out a
// token pair per device session, so several accounts can be stored side by side
// and rotated per request.
type OmniRushAccount struct {
	ID            string `json:"id"`
	Email         string `json:"email,omitempty"`
	AccessToken   string `json:"accessToken"`
	RefreshToken  string `json:"refreshToken"`
	GatewayURL    string `json:"gatewayUrl,omitempty"`
	SavedAt       string `json:"savedAt,omitempty"`
	Enabled       bool   `json:"enabled"`
	LastUsed      string `json:"lastUsed,omitempty"`
	LastError     string `json:"lastError,omitempty"`
	CooldownUntil string `json:"cooldownUntil,omitempty"`
	// LegacyPath mirrors rotations back to the file a co-installed OmniRush CLI
	// reads, so the CLI and this plugin never end up with two live token pairs.
	LegacyPath string `json:"legacyPath,omitempty"`
}

type omniAccountStore struct {
	Version  int                `json:"version"`
	Accounts []*OmniRushAccount `json:"accounts"`
}

var (
	omniAccountsMu    sync.RWMutex
	cachedOmniStore   *omniAccountStore
	lastOmniStoreRead time.Time
	omniAccountMu     sync.Mutex // serialises read-modify-write of the store
	omniAccountLocks  sync.Map   // per-account refresh mutexes
)

const omniAccountStoreVersion = 2

// omniAccountCooldown is how long a failing account is skipped. The free tier
// throttles per account, so a short bench is enough to route around it.
const (
	omniAccountCooldown  = 45 * time.Second
	omniAccountMaxTries  = 3
	omniAccountsCacheTTL = 2 * time.Second
)

func omniAccountsPath() string {
	if p := strings.TrimSpace(os.Getenv("OMNIRUSH_ACCOUNTS_FILE")); p != "" {
		return p
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return filepath.Join(home, ".cli-proxy-api", "omnirush-accounts.json")
	}
	return "omnirush-accounts.json"
}

func newOmniAccountID(email string) string {
	key := strings.ToLower(strings.TrimSpace(email))
	if key == "" {
		return ""
	}
	return key
}

func omniAccountIDFromToken(token string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(token)))
	return "token-" + hex.EncodeToString(sum[:6])
}

// ensureAccountID keeps older entries addressable: an account without an email
// still needs a stable handle for the UI and for rotation bookkeeping.
func ensureAccountID(acct *OmniRushAccount) {
	if acct == nil {
		return
	}
	if id := newOmniAccountID(acct.Email); id != "" {
		acct.ID = id
		return
	}
	if strings.TrimSpace(acct.ID) == "" {
		acct.ID = omniAccountIDFromToken(acct.RefreshToken + acct.AccessToken)
	}
}

func accountToAuth(acct *OmniRushAccount) *OmniRushAuth {
	return &OmniRushAuth{
		AccessToken:  acct.AccessToken,
		RefreshToken: acct.RefreshToken,
		GatewayURL:   acct.GatewayURL,
		SavedAt:      acct.SavedAt,
		Email:        acct.Email,
	}
}

func authToAccount(auth *OmniRushAuth) *OmniRushAccount {
	acct := &OmniRushAccount{
		Email:        strings.TrimSpace(auth.Email),
		AccessToken:  auth.AccessToken,
		RefreshToken: auth.RefreshToken,
		GatewayURL:   auth.GatewayURL,
		SavedAt:      auth.SavedAt,
		Enabled:      true,
	}
	ensureAccountID(acct)
	return acct
}

func (a *OmniRushAccount) label() string {
	if e := strings.TrimSpace(a.Email); e != "" {
		return e
	}
	return a.ID
}

func (a *OmniRushAccount) coolingDown() bool {
	if strings.TrimSpace(a.CooldownUntil) == "" {
		return false
	}
	until, err := time.Parse(time.RFC3339, a.CooldownUntil)
	if err != nil {
		return false
	}
	return time.Now().Before(until)
}

func (a *OmniRushAccount) status() string {
	switch {
	case !a.Enabled:
		return "disabled"
	case a.coolingDown():
		return "cooldown"
	case strings.TrimSpace(a.LastError) != "":
		return "error"
	default:
		return "active"
	}
}

// loadOmniAccounts reads the store and migrates a legacy single-account file on
// first use, so an upgrade never loses the existing session.
func loadOmniAccounts() (*omniAccountStore, error) {
	omniAccountsMu.RLock()
	if time.Since(lastOmniStoreRead) < omniAccountsCacheTTL && cachedOmniStore != nil {
		// Hand out a private copy: the cached object is published once and is
		// never mutated afterwards, so callers can read it without racing.
		store := cloneOmniAccounts(cachedOmniStore)
		omniAccountsMu.RUnlock()
		return store, nil
	}
	omniAccountsMu.RUnlock()

	omniAccountMu.Lock()
	defer omniAccountMu.Unlock()
	return loadOmniAccountsLocked()
}

func loadOmniAccountsLocked() (*omniAccountStore, error) {
	path := omniAccountsPath()
	data, err := os.ReadFile(path)
	if err == nil && len(data) > 0 {
		var store omniAccountStore
		if err := json.Unmarshal(data, &store); err == nil {
			normalized := &omniAccountStore{Version: omniAccountStoreVersion}
			for _, acct := range store.Accounts {
				if acct == nil || (acct.AccessToken == "" && acct.RefreshToken == "") {
					continue
				}
				ensureAccountID(acct)
				normalized.Accounts = append(normalized.Accounts, acct)
			}
			cacheOmniStore(normalized)
			return normalized, nil
		}
	}

	migrated, err := migrateLegacyOmniAccounts()
	if err != nil {
		return nil, err
	}
	cacheOmniStore(migrated)
	if len(migrated.Accounts) > 0 {
		if err := writeOmniAccountsLocked(migrated); err != nil {
			logMsg("omnirush: failed to persist migrated accounts: %v", err)
		} else {
			logMsg("omnirush: migrated %d legacy account(s) into %s", len(migrated.Accounts), path)
		}
	}
	return migrated, nil
}

// cloneOmniAccounts deep-copies the pool so a published store can never be
// mutated in place while another goroutine reads it.
func cloneOmniAccounts(store *omniAccountStore) *omniAccountStore {
	if store == nil {
		return nil
	}
	out := &omniAccountStore{
		Version:  store.Version,
		Accounts: make([]*OmniRushAccount, 0, len(store.Accounts)),
	}
	for _, acct := range store.Accounts {
		if acct == nil {
			continue
		}
		copied := *acct
		out.Accounts = append(out.Accounts, &copied)
	}
	return out
}

func cacheOmniStore(store *omniAccountStore) {
	// Publish a copy: the caller keeps mutating its own store object after the
	// write returns, and that must not be visible to readers.
	published := cloneOmniAccounts(store)
	omniAccountsMu.Lock()
	cachedOmniStore = published
	lastOmniStoreRead = time.Now()
	omniAccountsMu.Unlock()
}

func invalidateOmniStore() {
	omniAccountsMu.Lock()
	cachedOmniStore = nil
	lastOmniStoreRead = time.Time{}
	omniAccountsMu.Unlock()

	// The legacy cache feeds the single-account compatibility path.
	omniAuthMu.Lock()
	cachedOmniAuth = nil
	lastOmniAuthCheck = time.Time{}
	omniAuthMu.Unlock()
}

func migrateLegacyOmniAccounts() (*omniAccountStore, error) {
	store := &omniAccountStore{Version: omniAccountStoreVersion}

	if token := strings.TrimSpace(os.Getenv("OMNIRUSH_ACCESS_TOKEN")); token != "" {
		acct := &OmniRushAccount{
			Email:        "env-configured",
			AccessToken:  token,
			RefreshToken: strings.TrimSpace(os.Getenv("OMNIRUSH_REFRESH_TOKEN")),
			GatewayURL:   defaultOmniRushOrigin,
			SavedAt:      time.Now().UTC().Format(time.RFC3339),
			Enabled:      true,
		}
		ensureAccountID(acct)
		store.Accounts = append(store.Accounts, acct)
		return store, nil
	}

	seen := map[string]bool{}
	for _, path := range []string{getOmniRushAuthPath(), getOmniRushFallbackAuthPath()} {
		if path == "" || seen[path] {
			continue
		}
		seen[path] = true
		auth, _, err := loadOmniRushAuthFromFile(path)
		if err != nil || auth == nil {
			continue
		}
		acct := authToAccount(auth)
		acct.LegacyPath = path
		store.Accounts = append(store.Accounts, acct)
	}
	return store, nil
}

func writeOmniAccountsLocked(store *omniAccountStore) error {
	path := omniAccountsPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create omnirush dir: %w", err)
	}
	store.Version = omniAccountStoreVersion
	payload, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return err
	}
	payload = append(payload, '\n')

	tmp := fmt.Sprintf("%s.tmp.%d", path, time.Now().UnixNano())
	if err := os.WriteFile(tmp, payload, 0o600); err != nil {
		return fmt.Errorf("write omnirush accounts: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("replace omnirush accounts: %w", err)
	}
	cacheOmniStore(store)
	return nil
}

// saveOmniAccounts persists the store. Callers that already hold
// omniAccountMu use writeOmniAccountsLocked instead.
func saveOmniAccounts(store *omniAccountStore) error {
	omniAccountMu.Lock()
	defer omniAccountMu.Unlock()
	return writeOmniAccountsLocked(store)
}

// upsertOmniAccount stores a fresh token pair, matching on email so signing in
// again refreshes an existing entry instead of piling up duplicates.
func upsertOmniAccount(auth *OmniRushAuth, legacyPath string) (*OmniRushAccount, error) {
	if auth == nil || (auth.AccessToken == "" && auth.RefreshToken == "") {
		return nil, errors.New("refusing to store empty omnirush credentials")
	}
	if auth.SavedAt == "" {
		auth.SavedAt = time.Now().UTC().Format(time.RFC3339)
	}
	if auth.GatewayURL == "" {
		auth.GatewayURL = defaultOmniRushOrigin
	}

	omniAccountMu.Lock()
	store, err := loadOmniAccountsLocked()
	if err != nil {
		store = &omniAccountStore{Version: omniAccountStoreVersion}
	}

	incoming := authToAccount(auth)
	var target *OmniRushAccount
	for _, acct := range store.Accounts {
		if incoming.Email != "" && strings.EqualFold(acct.Email, incoming.Email) {
			target = acct
			break
		}
		if incoming.Email == "" && acct.ID == incoming.ID {
			target = acct
			break
		}
	}
	if target == nil {
		target = incoming
		store.Accounts = append(store.Accounts, target)
	} else {
		target.AccessToken = incoming.AccessToken
		target.RefreshToken = incoming.RefreshToken
		target.GatewayURL = incoming.GatewayURL
		target.SavedAt = incoming.SavedAt
		target.LastError = ""
		target.CooldownUntil = ""
		if target.Email == "" {
			target.Email = incoming.Email
			ensureAccountID(target)
		}
	}
	target.Enabled = true
	if legacyPath != "" {
		target.LegacyPath = legacyPath
	}

	if err := writeOmniAccountsLocked(store); err != nil {
		omniAccountMu.Unlock()
		invalidateOmniStore()
		return nil, err
	}
	copied := *target
	omniAccountMu.Unlock()

	invalidateOmniStore()
	return &copied, nil
}

// selectOmniAccount picks the least recently used healthy account. exclude lets
// a single request fail over without retrying the account that just failed.
func selectOmniAccount(exclude map[string]bool) (*OmniRushAccount, error) {
	store, err := loadOmniAccounts()
	if err != nil {
		return nil, err
	}
	if len(store.Accounts) == 0 {
		return nil, errors.New("no omnirush credentials found")
	}

	candidates := make([]*OmniRushAccount, 0, len(store.Accounts))
	for _, acct := range store.Accounts {
		if acct == nil || acct.AccessToken == "" {
			continue
		}
		if !acct.Enabled || acct.coolingDown() {
			continue
		}
		if exclude != nil && exclude[acct.ID] {
			continue
		}
		candidates = append(candidates, acct)
	}
	if len(candidates) == 0 {
		// Everything is benched or excluded: fall back to any enabled account so
		// a single-account setup keeps working instead of hard-failing.
		for _, acct := range store.Accounts {
			if acct != nil && acct.Enabled && acct.AccessToken != "" {
				if exclude != nil && exclude[acct.ID] {
					continue
				}
				candidates = append(candidates, acct)
			}
		}
	}
	if len(candidates) == 0 {
		return nil, errors.New("no usable omnirush account (all disabled or cooling down)")
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		li, lj := candidates[i].LastUsed, candidates[j].LastUsed
		if li == "" {
			return true
		}
		if lj == "" {
			return false
		}
		return li < lj
	})
	selected := *candidates[0]
	// Stamp LastUsed immediately so concurrent requests rotate across accounts
	// instead of all grabbing the same least-recently-used entry.
	touchOmniAccount(selected.ID)
	return &selected, nil
}

// updateOmniAccount applies a mutation to the stored account in one locked
// read-modify-write cycle.
func updateOmniAccount(id string, mutate func(*OmniRushAccount)) error {
	omniAccountMu.Lock()
	store, err := loadOmniAccountsLocked()
	if err != nil {
		omniAccountMu.Unlock()
		return err
	}
	var target *OmniRushAccount
	for _, acct := range store.Accounts {
		if acct.ID == id || strings.EqualFold(acct.Email, id) {
			target = acct
			break
		}
	}
	if target == nil {
		omniAccountMu.Unlock()
		return fmt.Errorf("unknown omnirush account %q", id)
	}
	mutate(target)
	err = writeOmniAccountsLocked(store)
	omniAccountMu.Unlock()
	invalidateOmniStore()
	return err
}

func touchOmniAccount(id string) {
	_ = updateOmniAccount(id, func(a *OmniRushAccount) {
		a.LastUsed = time.Now().UTC().Format(time.RFC3339)
		a.LastError = ""
		a.CooldownUntil = ""
	})
}

func penalizeOmniAccount(id, reason string, cooldown time.Duration) {
	_ = updateOmniAccount(id, func(a *OmniRushAccount) {
		a.LastError = reason
		if cooldown > 0 {
			a.CooldownUntil = time.Now().Add(cooldown).UTC().Format(time.RFC3339)
		}
	})
}

func setOmniAccountEnabled(id string, enabled bool) error {
	return updateOmniAccount(id, func(a *OmniRushAccount) {
		a.Enabled = enabled
		if enabled {
			a.LastError = ""
			a.CooldownUntil = ""
		}
	})
}

func removeOmniAccount(id string) error {
	omniAccountMu.Lock()
	store, err := loadOmniAccountsLocked()
	if err != nil {
		omniAccountMu.Unlock()
		return err
	}
	kept := make([]*OmniRushAccount, 0, len(store.Accounts))
	var removed *OmniRushAccount
	for _, acct := range store.Accounts {
		if acct.ID == id || strings.EqualFold(acct.Email, id) {
			removed = acct
			continue
		}
		kept = append(kept, acct)
	}
	if removed == nil {
		omniAccountMu.Unlock()
		return fmt.Errorf("unknown omnirush account %q", id)
	}
	store.Accounts = kept
	err = writeOmniAccountsLocked(store)
	omniAccountMu.Unlock()
	invalidateOmniStore()

	// Drop the CLI mirror only when no other account still points at it.
	if removed.LegacyPath != "" {
		stillUsed := false
		if fresh, errFresh := loadOmniAccounts(); errFresh == nil {
			for _, acct := range fresh.Accounts {
				if acct.LegacyPath == removed.LegacyPath {
					stillUsed = true
					break
				}
			}
		}
		if !stillUsed {
			_ = os.Remove(removed.LegacyPath)
		}
	}
	return err
}

// backfillOmniAccountLabels fills in a missing email by asking /device/me, so a
// migrated CLI session shows a human label instead of a token fingerprint. Best
// effort: failures are ignored and retried on the next status refresh.
func backfillOmniAccountLabels() {
	store, err := loadOmniAccounts()
	if err != nil {
		return
	}
	for _, acct := range store.Accounts {
		if acct == nil || strings.TrimSpace(acct.Email) != "" || acct.AccessToken == "" {
			continue
		}
		// Only query /device/me if the token is already valid. Never run a
		// blocking refresh during a background status poll.
		email := fetchOmniRushUserEmail(acct.AccessToken, acct.GatewayURL)
		if email == "" {
			continue
		}
		_ = updateOmniAccount(acct.ID, func(a *OmniRushAccount) {
			a.Email = email
			ensureAccountID(a)
		})
	}
}

func omniAccountsForUI() []map[string]any {
	store, err := loadOmniAccounts()
	if err != nil {
		return []map[string]any{}
	}
	out := make([]map[string]any, 0, len(store.Accounts))
	for _, acct := range store.Accounts {
		out = append(out, map[string]any{
			"id":            acct.ID,
			"email":         acct.Email,
			"label":         acct.label(),
			"enabled":       acct.Enabled,
			"status":        acct.status(),
			"lastUsed":      acct.LastUsed,
			"savedAt":       acct.SavedAt,
			"lastError":     acct.LastError,
			"cooldownUntil": acct.CooldownUntil,
		})
	}
	return out
}

// omniAccountRefreshLock returns a per-account mutex so two requests on the same
// account cannot spend the same refresh token twice, while other accounts keep
// refreshing in parallel.
func omniAccountRefreshLock(id string) *sync.Mutex {
	if v, ok := omniAccountLocks.Load(id); ok {
		return v.(*sync.Mutex)
	}
	actual, _ := omniAccountLocks.LoadOrStore(id, &sync.Mutex{})
	return actual.(*sync.Mutex)
}

func countEnabledOmniAccounts() int {
	store, err := loadOmniAccounts()
	if err != nil {
		return 0
	}
	n := 0
	for _, acct := range store.Accounts {
		if acct.Enabled && acct.AccessToken != "" {
			n++
		}
	}
	return n
}
