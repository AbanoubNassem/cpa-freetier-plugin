package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestMutatingResourcePathClassification(t *testing.T) {
	mutating := []string{
		"/v0/resource/plugins/cpa-freetier-plugin/api/omnirush/start",
		"/v0/resource/plugins/cpa-freetier-plugin/api/omnirush/disconnect",
		"/v0/resource/plugins/cpa-freetier-plugin/api/omnirush/import",
		"/v0/resource/plugins/cpa-freetier-plugin/api/omnirush/remove",
		"/v0/resource/plugins/cpa-freetier-plugin/api/omnirush/toggle",
		"/v0/resource/plugins/cpa-freetier-plugin/api/models/toggle",
		"/v0/resource/plugins/cpa-freetier-plugin/api/settings/save",
		"/v0/resource/plugins/cpa-freetier-plugin/api/settings/reset",
		"/v0/resource/plugins/cpa-freetier-plugin/api/proxy/test",
	}
	for _, p := range mutating {
		if !mutatingResourcePath(p) {
			t.Errorf("expected %s to be classified as mutating", p)
		}
	}

	readOnly := []string{
		"/v0/resource/plugins/cpa-freetier-plugin/dashboard",
		"/v0/resource/plugins/cpa-freetier-plugin/api/status",
		"/v0/resource/plugins/cpa-freetier-plugin/api/omnirush/poll",
		"/v0/resource/plugins/cpa-freetier-plugin/api/models/probe",
		"/v0/resource/plugins/cpa-freetier-plugin/api/proxy/ping",
	}
	for _, p := range readOnly {
		if mutatingResourcePath(p) {
			t.Errorf("expected %s to be read-only", p)
		}
	}
}

func TestMutatingEndpointRejectsMissingToken(t *testing.T) {
	req := managementRequestEnvelope{
		Method: "GET",
		Path:   "/v0/resource/plugins/cpa-freetier-plugin/api/models/toggle",
		Query: map[string][]string{
			"model":   {"gpt-6-sol"},
			"enabled": {"0"},
		},
	}
	raw, _ := json.Marshal(req)

	out, err := handleManagementHandle(raw)
	if err != nil {
		t.Fatalf("handleManagementHandle error: %v", err)
	}

	var env envelope
	if err := json.Unmarshal(out, &env); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if !env.OK {
		t.Fatalf("expected an OK envelope carrying the HTTP response, got %+v", env.Error)
	}

	var resp managementResponseEnvelope
	if err := json.Unmarshal(env.Result, &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.StatusCode != 403 {
		t.Errorf("expected 403 without the dashboard token, got %d", resp.StatusCode)
	}
	if !strings.Contains(string(resp.Body), "forbidden") {
		t.Errorf("expected a forbidden error body, got %s", string(resp.Body))
	}
	if isModelExcluded("gpt-6-sol") {
		t.Error("a rejected call must not change any model state")
	}
}

func TestMutatingEndpointRejectsWrongToken(t *testing.T) {
	req := managementRequestEnvelope{
		Method: "GET",
		Path:   "/v0/resource/plugins/cpa-freetier-plugin/api/omnirush/disconnect",
		Headers: map[string][]string{
			dashboardTokenHeader: {"deadbeef"},
		},
	}
	raw, _ := json.Marshal(req)

	out, _ := handleManagementHandle(raw)
	var env envelope
	_ = json.Unmarshal(out, &env)
	var resp managementResponseEnvelope
	_ = json.Unmarshal(env.Result, &resp)

	if resp.StatusCode != 403 {
		t.Errorf("expected 403 for a wrong token, got %d", resp.StatusCode)
	}
}

func TestReadOnlyEndpointNeedsNoToken(t *testing.T) {
	req := managementRequestEnvelope{
		Method: "GET",
		Path:   "/v0/resource/plugins/cpa-freetier-plugin/api/proxy/ping",
	}
	raw, _ := json.Marshal(req)

	out, err := handleManagementHandle(raw)
	if err != nil {
		t.Fatalf("handleManagementHandle error: %v", err)
	}
	var env envelope
	_ = json.Unmarshal(out, &env)
	if !env.OK {
		t.Fatalf("expected read-only path to succeed, got %+v", env.Error)
	}
	var resp managementResponseEnvelope
	_ = json.Unmarshal(env.Result, &resp)
	if resp.StatusCode != 200 {
		t.Errorf("expected 200 for proxy ping, got %d", resp.StatusCode)
	}
}

func TestRegisterDeclaresEachPathOnce(t *testing.T) {
	raw, err := handleManagementRegister(nil)
	if err != nil {
		t.Fatalf("handleManagementRegister error: %v", err)
	}
	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	var resp managementRegistrationResponse
	if err := json.Unmarshal(env.Result, &resp); err != nil {
		t.Fatalf("decode registration: %v", err)
	}

	// The host registers Routes before Resources and silently skips a duplicate,
	// so a path declared in both lists would drop its menu entry.
	seen := map[string]string{}
	for _, r := range resp.Resources {
		if prev, ok := seen[r.Path]; ok {
			t.Errorf("path %q declared twice (also as %q)", r.Path, prev)
		}
		seen[r.Path] = "resource"
	}
	for _, r := range resp.Routes {
		if prev, ok := seen[r.Path]; ok {
			t.Errorf("path %q declared both as %s and as a management route", r.Path, prev)
		}
		seen[r.Path] = "route"
	}

	if len(resp.Routes) != 0 {
		t.Errorf("expected no management routes (everything is served as a resource), got %d", len(resp.Routes))
	}
}
