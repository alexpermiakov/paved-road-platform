package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOrdersHandler(t *testing.T) {
	rr := httptest.NewRecorder()
	ordersHandler(rr, httptest.NewRequest(http.MethodGet, "/orders", nil))

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	if ct := rr.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}

	var body struct {
		Order struct {
			OrderID string `json:"order_id"`
			Status  string `json:"status"`
			Total   string `json:"total"`
			Items   int    `json:"items"`
		} `json:"order"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if !strings.HasPrefix(body.Order.OrderID, "ORD-") {
		t.Errorf("order_id = %q, want ORD- prefix", body.Order.OrderID)
	}
	allowed := map[string]bool{"pending": true, "confirmed": true, "processing": true, "shipped": true, "delivered": true}
	if !allowed[body.Order.Status] {
		t.Errorf("status = %q, not in allowed set", body.Order.Status)
	}
	if body.Order.Items < 1 || body.Order.Items > 10 {
		t.Errorf("items = %d, want 1..10", body.Order.Items)
	}
}

func TestOrdersHandlerIDChanges(t *testing.T) {
	// order_id is derived from a monotonic request counter, so consecutive
	// calls must produce different IDs.
	if a, b := orderID(t), orderID(t); a == b {
		t.Errorf("order_id did not advance between calls: %q", a)
	}
}

func orderID(t *testing.T) string {
	t.Helper()
	rr := httptest.NewRecorder()
	ordersHandler(rr, httptest.NewRequest(http.MethodGet, "/orders", nil))
	var body struct {
		Order struct {
			OrderID string `json:"order_id"`
		} `json:"order"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return body.Order.OrderID
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
	if body["service"] != "order-service" {
		t.Errorf("service = %q, want order-service", body["service"])
	}
}

func TestGetEnv(t *testing.T) {
	if got := getEnv("ORDER_SVC_UNSET", "fallback"); got != "fallback" {
		t.Errorf("getEnv(unset) = %q, want fallback", got)
	}
	t.Setenv("ORDER_SVC_SET", "value")
	if got := getEnv("ORDER_SVC_SET", "fallback"); got != "value" {
		t.Errorf("getEnv(set) = %q, want value", got)
	}
}
