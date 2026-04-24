package management

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestServerIndexReturnsJSON(t *testing.T) {
	server := NewServer(Options{
		ListenPort: 9091,
		Version:    "1.2.3",
		Features:   []string{"web-rules", "management"},
		StatusProvider: func() RuleStatusSummary {
			return RuleStatusSummary{
				Web:        RuleState{Enabled: true, Loaded: true},
				Custom:     RuleState{Enabled: true, Loaded: false},
				AutoDetect: RuleState{Enabled: false, Loaded: false},
			}
		},
	})

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/", nil)

	server.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("unexpected status code: got %d want %d", recorder.Code, http.StatusOK)
	}
	if got := recorder.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("unexpected content type: got %q want %q", got, "application/json")
	}

	var response IndexResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if !response.Success {
		t.Fatal("expected success response")
	}
	if response.Version != "1.2.3" {
		t.Fatalf("unexpected version: got %q want %q", response.Version, "1.2.3")
	}
	if len(response.Features) != 2 {
		t.Fatalf("unexpected features length: got %d want %d", len(response.Features), 2)
	}
	if !response.Rules.Web.Enabled || !response.Rules.Web.Loaded {
		t.Fatalf("unexpected web status: %+v", response.Rules.Web)
	}
}

func TestServerIndexRejectsUnsupportedMethod(t *testing.T) {
	server := NewServer(Options{})

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/", nil)

	server.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("unexpected status code: got %d want %d", recorder.Code, http.StatusMethodNotAllowed)
	}
}

func TestReloadWebRulesCallsReloader(t *testing.T) {
	calls := 0
	server := NewServer(Options{
		ReloadWebRules: func(context.Context) error {
			calls++
			return nil
		},
	})

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/reload_web_rules", nil)

	server.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("unexpected status code: got %d want %d", recorder.Code, http.StatusOK)
	}
	if calls != 1 {
		t.Fatalf("unexpected call count: got %d want %d", calls, 1)
	}
}

func TestReloadWebRulesRejectsUnsupportedMethod(t *testing.T) {
	server := NewServer(Options{})

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/reload_web_rules", nil)

	server.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("unexpected status code: got %d want %d", recorder.Code, http.StatusMethodNotAllowed)
	}
}

func TestReloadWebRulesReturnsConfigurationErrorWhenHandlerMissing(t *testing.T) {
	server := NewServer(Options{})

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/reload_web_rules", nil)

	server.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("unexpected status code: got %d want %d", recorder.Code, http.StatusInternalServerError)
	}
}

func TestReloadCustomRulesCallsBothReloaders(t *testing.T) {
	customCalls := 0
	server := NewServer(Options{
		ReloadCustomRules: func(context.Context) error {
			customCalls++
			return nil
		},
	})

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/reload_custom_rules", nil)

	server.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("unexpected status code: got %d want %d", recorder.Code, http.StatusOK)
	}
	if customCalls != 1 {
		t.Fatalf("unexpected custom call count: got %d want %d", customCalls, 1)
	}

	var response ActionResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if !response.Success || response.Partial {
		t.Fatalf("unexpected response flags: %+v", response)
	}
	if len(response.Steps) != 1 {
		t.Fatalf("unexpected step count: got %d want %d", len(response.Steps), 1)
	}
}

func TestReloadCustomRulesRejectsUnsupportedMethod(t *testing.T) {
	server := NewServer(Options{})

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/reload_custom_rules", nil)

	server.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("unexpected status code: got %d want %d", recorder.Code, http.StatusMethodNotAllowed)
	}
}

func TestReloadCustomRulesReturnsPartialFailure(t *testing.T) {
	customErr := errors.New("custom reload failed")
	customCalls := 0
	server := NewServer(Options{
		ReloadCustomRules: func(context.Context) error {
			customCalls++
			return customErr
		},
	})

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/reload_custom_rules", nil)

	server.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("unexpected status code: got %d want %d", recorder.Code, http.StatusInternalServerError)
	}
	if customCalls != 1 {
		t.Fatalf("unexpected custom call count: got %d want %d", customCalls, 1)
	}

	var response ActionResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if response.Success {
		t.Fatal("expected failure response")
	}
	if response.Partial {
		t.Fatalf("expected single-step failure to be non-partial, got %+v", response)
	}
	if response.Steps[0].Error == "" {
		t.Fatalf("expected custom failure detail, got %+v", response.Steps)
	}
}

func TestReloadRulesCallsAllReloaders(t *testing.T) {
	webCalls := 0
	customCalls := 0
	server := NewServer(Options{
		ReloadWebRules: func(context.Context) error {
			webCalls++
			return nil
		},
		ReloadCustomRules: func(context.Context) error {
			customCalls++
			return nil
		},
	})

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/reload_rules", nil)

	server.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("unexpected status code: got %d want %d", recorder.Code, http.StatusOK)
	}
	if webCalls != 1 || customCalls != 1 {
		t.Fatalf("unexpected call counts: web=%d custom=%d", webCalls, customCalls)
	}
}

func TestReloadRulesReturnsPartialFailureWhenCustomReloadFails(t *testing.T) {
	server := NewServer(Options{
		ReloadWebRules: func(context.Context) error { return nil },
		ReloadCustomRules: func(context.Context) error {
			return errors.New("custom reload failed")
		},
	})

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/reload_rules", nil)

	server.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("unexpected status code: got %d want %d", recorder.Code, http.StatusInternalServerError)
	}

	var response ActionResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if response.Success {
		t.Fatal("expected failure response")
	}
	if !response.Partial {
		t.Fatalf("expected partial failure, got %+v", response)
	}
	if len(response.Steps) != 2 {
		t.Fatalf("unexpected step count: got %d want %d", len(response.Steps), 2)
	}
	if !response.Steps[0].Success || response.Steps[1].Error == "" {
		t.Fatalf("unexpected steps: %+v", response.Steps)
	}
}

func TestReloadRulesRejectsUnsupportedMethod(t *testing.T) {
	server := NewServer(Options{})

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/reload_rules", nil)

	server.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("unexpected status code: got %d want %d", recorder.Code, http.StatusMethodNotAllowed)
	}
}

func TestFirstStepErrorReturnsDefaultMessageWhenMissing(t *testing.T) {
	if got := firstStepError([]ActionStep{{Name: "ok", Success: true}}); got != "request failed" {
		t.Fatalf("unexpected error message: got %q want %q", got, "request failed")
	}
}
