package handlers

import (
	"log"
	"net/http"

	"github.com/cloudzz-dev/cldzmsg/internal/server/ratelimit"
	"github.com/cloudzz-dev/cldzmsg/internal/server/ws"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

func HealthCheck(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}

func HandleWebSocket(hub *ws.Hub, limiter *ratelimit.RateLimiter, w http.ResponseWriter, r *http.Request) {
	clientIP := ratelimit.GetClientIP(r)

	// Rate limit: check connection count per IP
	if !limiter.CanConnect(clientIP) {
		http.Error(w, "Too many connections from your IP", http.StatusTooManyRequests)
		log.Printf("Rate limited connection from %s", clientIP)
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("Upgrade error:", err)
		return
	}

	limiter.AddConnection(clientIP)

	client := &ws.Client{
		Hub:      hub,
		Conn:     conn,
		Send:     make(chan []byte, 256),
		Limiter:  limiter,
		IP:       clientIP,
		UserID:   0, // Not authenticated yet
		Username: "",
	}

	// Writer goroutine
	go func() {
		defer limiter.RemoveConnection(clientIP)
		client.WritePump()
	}()

	// Reader goroutine
	go client.ReadPump()
}
