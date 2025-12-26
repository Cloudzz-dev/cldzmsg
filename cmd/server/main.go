package main

import (
	"log"
	"net/http"
	"os"

	"github.com/cloudzz-dev/cldzmsg/internal/server/handlers"
	"github.com/cloudzz-dev/cldzmsg/internal/server/ratelimit"
	"github.com/cloudzz-dev/cldzmsg/internal/server/storage"
	"github.com/cloudzz-dev/cldzmsg/internal/server/ws"
)

func main() {
	// Initialize Storage (DB)
	store := storage.New()
	defer store.Close()

	// Initialize Rate Limiter
	limiter := ratelimit.New()

	// Initialize WebSocket Hub
	hub := ws.NewHub(store)
	go hub.Run()

	// Routes
	http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		handlers.HandleWebSocket(hub, limiter, w, r)
	})

	http.HandleFunc("/health", handlers.HealthCheck)

	port := os.Getenv("PORT")
	if port == "" {
		port = "3567"
	}

	log.Printf("Server starting on :%s", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}