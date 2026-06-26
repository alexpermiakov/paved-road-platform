package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAskHandlerMissingPrompt(t *testing.T) {
	rr := httptest.NewRecorder()
	askHandler(rr, httptest.NewRequest(http.MethodGet, "/ask", nil))

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rr.Code)
	}
}

func TestAskHandlerSuccess(t *testing.T) {
	// Stand in for the vLLM OpenAI-compatible server.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/completions" {
			t.Errorf("upstream path = %q, want /v1/completions", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]string{{"text": "hello from vllm"}},
		})
	}))
	defer srv.Close()

	restore := vllmURL
	vllmURL = srv.URL
	defer func() { vllmURL = restore }()

	rr := httptest.NewRecorder()
	askHandler(rr, httptest.NewRequest(http.MethodGet, "/ask?prompt=hi", nil))

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	var body struct {
		Prompt   string `json:"prompt"`
		Response string `json:"response"`
		Model    string `json:"model"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Prompt != "hi" {
		t.Errorf("prompt = %q, want hi", body.Prompt)
	}
	if body.Response != "hello from vllm" {
		t.Errorf("response = %q, want %q", body.Response, "hello from vllm")
	}
}

func TestAskHandlerUpstreamError(t *testing.T) {
	// When vLLM returns a non-200, the client surfaces a 502 Bad Gateway.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"boom"}`))
	}))
	defer srv.Close()

	restore := vllmURL
	vllmURL = srv.URL
	defer func() { vllmURL = restore }()

	rr := httptest.NewRecorder()
	askHandler(rr, httptest.NewRequest(http.MethodGet, "/ask?prompt=hi", nil))

	if rr.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", rr.Code)
	}
}

func TestProbeHandlers(t *testing.T) {
	cases := []struct {
		name    string
		handler http.HandlerFunc
		want    string
	}{
		{"healthz", healthHandler, "OK"},
		{"readyz", readyHandler, "Ready"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rr := httptest.NewRecorder()
			tc.handler(rr, httptest.NewRequest(http.MethodGet, "/"+tc.name, nil))
			if rr.Code != http.StatusOK {
				t.Errorf("status = %d, want 200", rr.Code)
			}
			if got := strings.TrimSpace(rr.Body.String()); got != tc.want {
				t.Errorf("body = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestIndexHandler(t *testing.T) {
	rr := httptest.NewRecorder()
	indexHandler(rr, httptest.NewRequest(http.MethodGet, "/", nil))

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	var body map[string]string
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body["service"] != "llm-client" {
		t.Errorf("service = %q, want llm-client", body["service"])
	}
}

func TestGetEnv(t *testing.T) {
	if got := getEnv("LLM_CLIENT_UNSET", "fallback"); got != "fallback" {
		t.Errorf("getEnv(unset) = %q, want fallback", got)
	}
	t.Setenv("LLM_CLIENT_SET", "value")
	if got := getEnv("LLM_CLIENT_SET", "fallback"); got != "value" {
		t.Errorf("getEnv(set) = %q, want value", got)
	}
}
