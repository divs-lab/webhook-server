package handlers

import (
	"log/slog"
	"net/http"
)

// Health reports that the process is up.
func Health(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write([]byte(`{"status":"ok"}`)); err != nil {
		slog.Error("write health response", "error", err)
	}
}
