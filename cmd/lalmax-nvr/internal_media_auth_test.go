package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestInternalMediaControlAuth(t *testing.T) {
	const token = "test-internal-token"

	// 模拟外部用户认证：未携带用户凭据时拒绝。
	userAuth := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
		})
	}

	handler := newInternalMediaControlAuth(token, userAuth)(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
	)

	tests := []struct {
		name       string
		remoteAddr string
		token      string
		want       int
	}{
		{"valid loopback token", "127.0.0.1:12345", token, 200},
		{"no token", "127.0.0.1:12345", "", 401},
		{"wrong token", "127.0.0.1:12345", "wrong", 401},
		{"external address", "192.0.2.10:12345", token, 401},
		{"ipv6 loopback", "[::1]:12345", token, 200},
		{"malformed address", "invalid", token, 401},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/stat/lal_info", nil)
			req.RemoteAddr = tc.remoteAddr
			if tc.token != "" {
				req.Header.Set("X-Lalmax-Internal-Token", tc.token)
			}

			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d", rec.Code, tc.want)
			}
		})
	}
}
