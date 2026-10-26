package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// settingsPayload is what the dashboard renders: the stored values, the values
// actually in force after env fallback, and the file backing them.
func settingsPayload() map[string]any {
	cfg := loadPluginSettings()

	effective := map[string]any{
		"opencodeProxy":     "",
		"opencodePool":      []string{},
		"datacenterPool":    []string{},
		"allowDatacenter":   datacenterAllowed(),
		"omnirushProxy":     "",
		"sidecar":           getOpenCodeBaseURL(),
		"headerTimeout":     int(settingsHeaderTimeout() / time.Second),
		"omnirushTimeout":   int(settingsOmniRushTimeout() / time.Second),
		"omnirushReasoning": settingsOmniRushReasoning(),
	}

	if u := getOpenCodeProxy(); u != nil {
		effective["opencodeProxy"] = u.String()
	}
	pool := []string{}
	for _, u := range proxyPool("OPENCODE_PROXY_POOL", defaultOpenCodeProxies) {
		pool = append(pool, u.String())
	}
	effective["opencodePool"] = pool
	dcPool := []string{}
	for _, u := range proxyPool("OPENCODE_DATACENTER_PROXY_POOL", defaultOpenCodeDatacenterProxies) {
		dcPool = append(dcPool, u.String())
	}
	effective["datacenterPool"] = dcPool
	if v := effectiveOmniRushProxy(); v != "" {
		effective["omnirushProxy"] = v
	}

	return map[string]any{
		"status":    "ok",
		"settings":  cfg,
		"defaults":  defaultPluginSettings(),
		"effective": effective,
		"hints":     settingsEnvHints(),
		"path":      settingsPath(),
	}
}

// effectiveOmniRushProxy resolves the egress OmniRush calls actually use.
func effectiveOmniRushProxy() string {
	if v, ok := settingsProxyValue(loadPluginSettings().Proxy.OmniRush); ok {
		if v == "" {
			return proxyDisabledSentinel
		}
		return v
	}
	return strings.TrimSpace(os.Getenv("OMNIRUSH_PROXY"))
}

func handleSettingsGet() ([]byte, error) {
	data, err := json.Marshal(settingsPayload())
	if err != nil {
		return jsonErrorResponse(http.StatusInternalServerError, "encode_failed", err.Error())
	}
	return managementResponse(http.StatusOK, "application/json", data)
}

// handleSettingsSavePayload accepts the settings JSON as a base64url query value
// because the host only routes GET/HEAD on plugin resource paths, so a POST body
// never reaches this handler.
func handleSettingsSavePayload(q url.Values) ([]byte, error) {
	raw := strings.TrimSpace(q.Get("payload"))
	if raw == "" {
		return jsonErrorResponse(http.StatusBadRequest, "empty_payload", "expected a base64url-encoded settings payload")
	}
	body, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return jsonErrorResponse(http.StatusBadRequest, "invalid_payload", "payload is not valid base64url")
	}
	return handleSettingsSaveBytes(body)
}

func handleSettingsSaveBytes(body []byte) ([]byte, error) {
	if len(body) == 0 {
		return jsonErrorResponse(http.StatusBadRequest, "empty_body", "expected a settings JSON body")
	}
	var incoming PluginSettings
	if err := json.Unmarshal(body, &incoming); err != nil {
		return jsonErrorResponse(http.StatusBadRequest, "invalid_body", err.Error())
	}
	if err := savePluginSettings(&incoming); err != nil {
		return jsonErrorResponse(http.StatusBadRequest, "invalid_settings", err.Error())
	}
	data, err := json.Marshal(settingsPayload())
	if err != nil {
		return jsonErrorResponse(http.StatusInternalServerError, "encode_failed", err.Error())
	}
	return managementResponse(http.StatusOK, "application/json", data)
}

func handleSettingsReset() ([]byte, error) {
	if _, err := resetPluginSettings(); err != nil {
		return jsonErrorResponse(http.StatusInternalServerError, "reset_failed", err.Error())
	}
	data, err := json.Marshal(settingsPayload())
	if err != nil {
		return jsonErrorResponse(http.StatusInternalServerError, "encode_failed", err.Error())
	}
	return managementResponse(http.StatusOK, "application/json", data)
}

// handleProxyTest checks reachability the way the request path uses it: TCP
// first (cheap, proves the listener is up), then one HTTPS request through the
// proxy so a policy-level block shows up here instead of mid-conversation.
func handleProxyTest(q url.Values) ([]byte, error) {
	proxyStr := strings.TrimSpace(q.Get("proxy"))
	target := strings.TrimSpace(q.Get("target"))
	if target == "" {
		target = "https://opencode.ai/"
	}

	result := map[string]any{
		"proxy":  proxyStr,
		"target": target,
		"status": "ok",
	}
	finish := func() ([]byte, error) {
		data, _ := json.Marshal(result)
		return managementResponse(http.StatusOK, "application/json", data)
	}

	if proxyStr == "" || proxyStr == proxyDisabledSentinel {
		result["stage"] = "direct"
		return finish()
	}

	raw := proxyStr
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	pu, err := url.Parse(raw)
	if err != nil || !validProxyTarget(pu) {
		result["status"] = "error"
		result["error"] = fmt.Sprintf("%q is not a usable proxy address", proxyStr)
		return finish()
	}

	start := time.Now()
	conn, err := net.DialTimeout("tcp", pu.Host, 3*time.Second)
	if err != nil {
		result["status"] = "error"
		result["stage"] = "tcp"
		result["error"] = err.Error()
		return finish()
	}
	_ = conn.Close()
	result["tcpMs"] = time.Since(start).Milliseconds()

	client := &http.Client{
		Transport: &http.Transport{Proxy: http.ProxyURL(pu)},
		Timeout:   12 * time.Second,
	}
	req, err := http.NewRequest(http.MethodGet, target, nil)
	if err != nil {
		result["status"] = "error"
		result["stage"] = "http"
		result["error"] = err.Error()
		return finish()
	}
	req.Header.Set("User-Agent", loadPluginSettings().Identity.UserAgent)

	start = time.Now()
	resp, errHTTP := client.Do(req)
	if errHTTP != nil {
		result["status"] = "error"
		result["stage"] = "http"
		result["error"] = errHTTP.Error()
		return finish()
	}
	_, _ = io.CopyN(io.Discard, resp.Body, 1<<16)
	_ = resp.Body.Close()
	result["httpStatus"] = resp.StatusCode
	result["httpMs"] = time.Since(start).Milliseconds()
	return finish()
}
