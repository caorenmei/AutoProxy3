package cli

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestClientReloadCustomRulesUsesExpectedPath(t *testing.T) {
	requests := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests <- r.Method + " " + r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true}`))
	}))
	defer server.Close()

	client := newClient(server.URL, server.Client())
	if err := client.ReloadCustomRules(context.Background()); err != nil {
		t.Fatalf("reload custom rules: %v", err)
	}

	if got := <-requests; got != "POST /reload_custom_rules" {
		t.Fatalf("unexpected request: got %q want %q", got, "POST /reload_custom_rules")
	}
}

func TestNewClientReloadWebRulesUsesExpectedPath(t *testing.T) {
	requests := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests <- r.Method + " " + r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, nil)
	if err := client.ReloadWebRules(context.Background()); err != nil {
		t.Fatalf("reload web rules: %v", err)
	}

	if got := <-requests; got != "POST /reload_web_rules" {
		t.Fatalf("unexpected request: got %q want %q", got, "POST /reload_web_rules")
	}
}

func TestClientReloadRulesUsesExpectedPath(t *testing.T) {
	requests := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests <- r.Method + " " + r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true}`))
	}))
	defer server.Close()

	client := newClient(server.URL, server.Client())
	if err := client.ReloadRules(context.Background()); err != nil {
		t.Fatalf("reload rules: %v", err)
	}

	if got := <-requests; got != "POST /reload_rules" {
		t.Fatalf("unexpected request: got %q want %q", got, "POST /reload_rules")
	}
}

func TestClientReturnsErrorOnServerFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"success":false,"error":"reload failed"}`))
	}))
	defer server.Close()

	client := newClient(server.URL, server.Client())
	err := client.ReloadCustomRules(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "reload failed") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestClientReturnsDecodeErrorOnInvalidPayload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{`))
	}))
	defer server.Close()

	client := newClient(server.URL, server.Client())
	err := client.ReloadCustomRules(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "decode response") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestClientReturnsBuildRequestErrorOnInvalidBaseURL(t *testing.T) {
	client := newClient("://bad", http.DefaultClient)
	err := client.ReloadCustomRules(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "build request") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestClientReturnsSendRequestError(t *testing.T) {
	client := newClient("http://example.com", &http.Client{
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("network down")
		}),
	})

	err := client.ReloadCustomRules(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "send request") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestClientReturnsStepErrorWhenTopLevelErrorMissing(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"success":false,"steps":[{"name":"reload_custom_rules","error":"step failed"}]}`))
	}))
	defer server.Close()

	client := newClient(server.URL, server.Client())
	err := client.ReloadCustomRules(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "step failed") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestClientReturnsStatusErrorWhenPayloadHasNoDetails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"success":false}`))
	}))
	defer server.Close()

	client := newClient(server.URL, server.Client())
	err := client.ReloadCustomRules(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "request failed with status 500") {
		t.Fatalf("unexpected error: %v", err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestRoundTripFuncRoundTrip(t *testing.T) {
	response, err := roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader("")),
		}, nil
	}).RoundTrip(httptest.NewRequest(http.MethodGet, "http://example.com", nil))
	if err != nil {
		t.Fatalf("round trip: %v", err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("unexpected status code: got %d want %d", response.StatusCode, http.StatusOK)
	}
}
