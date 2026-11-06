package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRequireAuth(t *testing.T) {
	apiKey := "secret-test-key"
	dummyHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"protected"}`))
	})

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	mux.Handle("GET /protected", dummyHandler)

	handler := RequireAuth(apiKey)(mux)

	tests := []struct {
		name       string
		path       string
		authHeader string
		wantStatus int
		wantErr    string
	}{
		{
			name:       "health endpoint bypasses authentication",
			path:       "/health",
			authHeader: "",
			wantStatus: http.StatusOK,
		},
		{
			name:       "missing authorization header",
			path:       "/protected",
			authHeader: "",
			wantStatus: http.StatusUnauthorized,
			wantErr:    "missing authorization header",
		},
		{
			name:       "invalid header format without Bearer prefix",
			path:       "/protected",
			authHeader: "Basic " + apiKey,
			wantStatus: http.StatusUnauthorized,
			wantErr:    "invalid authorization header format",
		},
		{
			name:       "wrong api key",
			path:       "/protected",
			authHeader: "Bearer wrong-key",
			wantStatus: http.StatusUnauthorized,
			wantErr:    "invalid api key",
		},
		{
			name:       "correct api key",
			path:       "/protected",
			authHeader: "Bearer " + apiKey,
			wantStatus: http.StatusOK,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			if tc.authHeader != "" {
				req.Header.Set("Authorization", tc.authHeader)
			}
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tc.wantStatus)
			}

			if tc.wantErr != "" {
				var errResp map[string]string
				if err := json.Unmarshal(rec.Body.Bytes(), &errResp); err != nil {
					t.Fatalf("failed to decode error response: %v", err)
				}
				if errResp["error"] != tc.wantErr {
					t.Fatalf("error = %q, want %q", errResp["error"], tc.wantErr)
				}
			}
		})
	}
}
