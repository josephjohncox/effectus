package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync/atomic"
	"time"
)

var caseCounter int64

func main() {
	port := getenv("PORT", "8080")

	mux := http.NewServeMux()
	mux.HandleFunc("/cases", handleCase)
	mux.HandleFunc("/healthz", handleHealth)

	server := &http.Server{
		Addr:    ":" + port,
		Handler: loggingMiddleware(mux),
	}

	log.Printf("risk-mock listening on %s", server.Addr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("risk-mock error: %v", err)
	}
}

func handleCase(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var payload map[string]interface{}
	_ = json.NewDecoder(r.Body).Decode(&payload)

	caseID := fmt.Sprintf("case-%d", atomic.AddInt64(&caseCounter, 1))
	response := map[string]interface{}{
		"caseId":    caseID,
		"status":    "opened",
		"opened_at": time.Now().UTC().Format(time.RFC3339),
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Printf("risk-mock %s %s", r.Method, r.URL.Path)
		next.ServeHTTP(w, r)
	})
}

func getenv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
