package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	_ "github.com/lib/pq"
	"golang.org/x/crypto/bcrypt"
)

// --- Rate Limiter ---

type RateLimiter struct {
	connections  map[string]int         // IP -> connection count
	authAttempts map[string][]time.Time // IP -> timestamps of auth attempts
	mu           sync.RWMutex
	maxConns     int
	maxAuth      int
}

func newRateLimiter() *RateLimiter {
	maxConns := 10
	if v := os.Getenv("MAX_CONNECTIONS_PER_IP"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			maxConns = n
		}
	}

	maxAuth := 5
	if v := os.Getenv("AUTH_ATTEMPTS_PER_MIN"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			maxAuth = n
		}
	}

	rl := &RateLimiter{
		connections:  make(map[string]int),
		authAttempts: make(map[string][]time.Time),
		maxConns:     maxConns,
		maxAuth:      maxAuth,
	}

	// Cleanup old auth attempts every minute
	go func() {
		for {
			time.Sleep(time.Minute)
			rl.cleanup()
		}
	}()

	return rl
}

func (rl *RateLimiter) cleanup() {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	cutoff := time.Now().Add(-time.Minute)
	for ip, attempts := range rl.authAttempts {
		var valid []time.Time
		for _, t := range attempts {
			if t.After(cutoff) {
				valid = append(valid, t)
			}
		}
		if len(valid) == 0 {
			delete(rl.authAttempts, ip)
		} else {
			rl.authAttempts[ip] = valid
		}
	}
}

func (rl *RateLimiter) canConnect(ip string) bool {
	rl.mu.RLock()
	defer rl.mu.RUnlock()
	return rl.connections[ip] < rl.maxConns
}

func (rl *RateLimiter) addConnection(ip string) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	rl.connections[ip]++
}

func (rl *RateLimiter) removeConnection(ip string) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	rl.connections[ip]--
	if rl.connections[ip] <= 0 {
		delete(rl.connections, ip)
	}
}

func (rl *RateLimiter) canAuth(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	cutoff := time.Now().Add(-time.Minute)
	var recent []time.Time
	for _, t := range rl.authAttempts[ip] {
		if t.After(cutoff) {
			recent = append(recent, t)
		}
	}
	rl.authAttempts[ip] = recent

	if len(recent) >= rl.maxAuth {
		return false
	}

	rl.authAttempts[ip] = append(rl.authAttempts[ip], time.Now())
	return true
}

func getClientIP(r *http.Request) string {
	// Check X-Forwarded-For header (for reverse proxies)
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		return xff
	}
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return xri
	}
	ip, _, _ := net.SplitHostPort(r.RemoteAddr)
	return ip
}

var rateLimiter *RateLimiter

// --- Models ---

type User struct {
	ID           int       `json:"id"`
	Username     string    `json:"username"`
	PasswordHash string    `json:"-"`
	CreatedAt    time.Time `json:"created_at"`
}

type Message struct {
	ID             int       `json:"id"`
	ConversationID int       `json:"conversation_id"`
	SenderID       int       `json:"sender_id"`
	SenderUsername string    `json:"sender_username,omitempty"`
	Content        string    `json:"content"`
	CreatedAt      time.Time `json:"created_at"`
}

type ReadReceiptPayload struct {
	ConversationID int `json:"conversation_id"`
}

// ... existing types ...

type Conversation struct {
	ID           int       `json:"id"`
	Name         *string   `json:"name,omitempty"`
	IsGroup      bool      `json:"is_group"`
	CreatedAt    time.Time `json:"created_at"`
	Participants []string  `json:"participants,omitempty"`
	UnreadCount  int       `json:"unread_count"` // New field
}

// --- WebSocket Hub ---

type Client struct {
	conn     *websocket.Conn
	userID   int
	username string
	send     chan []byte
}

type Hub struct {
	clients    map[*Client]bool
	broadcast  chan []byte
	register   chan *Client
	unregister chan *Client
	mu         sync.RWMutex
}

func newHub() *Hub {
	return &Hub{
		clients:    make(map[*Client]bool),
		broadcast:  make(chan []byte),
		register:   make(chan *Client),
		unregister: make(chan *Client),
	}
}

func (h *Hub) run() {
	for {
		select {
		case client := <-h.register:
			h.mu.Lock()
			h.clients[client] = true
			h.mu.Unlock()
		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.send)
			}
			h.mu.Unlock()
		case message := <-h.broadcast:
			h.mu.RLock()
			for client := range h.clients {
				select {
				case client.send <- message:
				default:
					close(client.send)
					delete(h.clients, client)
				}
			}
			h.mu.RUnlock()
		}
	}
}

// --- WebSocket Message Types ---

type WSMessage struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

type AuthPayload struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Action   string `json:"action"` // "login" or "register"
}

type SendMessagePayload struct {
	ConversationID int    `json:"conversation_id"`
	Content        string `json:"content"`
}

type CreateConversationPayload struct {
	Name      string   `json:"name,omitempty"`
	IsGroup   bool     `json:"is_group"`
	Usernames []string `json:"usernames"`
}

type CheckUserPayload struct {
	Username string `json:"username"`
}

// --- Database ---

var db *sql.DB

func initDB() {
	connStr := os.Getenv("DATABASE_URL")
	if connStr == "" {
		connStr = "postgres://localhost/cldzmsg?sslmode=disable"
	}

	var err error
	db, err = sql.Open("postgres", connStr)
	if err != nil {
		log.Fatal("Failed to connect to database:", err)
	}

	if err = db.Ping(); err != nil {
		log.Fatal("Failed to ping database:", err)
	}

	log.Println("Connected to database")
}

// --- Auth Handlers ---

func handleAuth(payload AuthPayload) (int, string, error) {
	if payload.Action == "register" {
		hash, err := bcrypt.GenerateFromPassword([]byte(payload.Password), bcrypt.DefaultCost)
		if err != nil {
			return 0, "", err
		}

		var userID int
		err = db.QueryRow(
			"INSERT INTO users (username, password_hash) VALUES ($1, $2) RETURNING id",
			payload.Username, string(hash),
		).Scan(&userID)
		if err != nil {
			return 0, "", err
		}

		return userID, payload.Username, nil
	}

	// Login
	var userID int
	var hash string
	err := db.QueryRow(
		"SELECT id, password_hash FROM users WHERE username = $1",
		payload.Username,
	).Scan(&userID, &hash)
	if err != nil {
		return 0, "", err
	}

	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(payload.Password)); err != nil {
		return 0, "", err
	}

	return userID, payload.Username, nil
}

func checkUserExists(username string) (bool, int) {
	var userID int
	err := db.QueryRow("SELECT id FROM users WHERE username = $1", username).Scan(&userID)
	if err != nil {
		return false, 0
	}
	return true, userID
}

// --- Conversation Handlers ---

func createConversation(creatorID int, payload CreateConversationPayload) (*Conversation, error) {
	tx, err := db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var convID int
	var name *string
	if payload.Name != "" {
		name = &payload.Name
	}

	err = tx.QueryRow(
		"INSERT INTO conversations (name, is_group) VALUES ($1, $2) RETURNING id",
		name, payload.IsGroup,
	).Scan(&convID)
	if err != nil {
		return nil, err
	}

	// Add creator
	_, err = tx.Exec(
		"INSERT INTO conversation_participants (conversation_id, user_id) VALUES ($1, $2)",
		convID, creatorID,
	)
	if err != nil {
		return nil, err
	}

	// Add other participants
	for _, username := range payload.Usernames {
		exists, userID := checkUserExists(username)
		if exists {
			tx.Exec(
				"INSERT INTO conversation_participants (conversation_id, user_id) VALUES ($1, $2) ON CONFLICT DO NOTHING",
				convID, userID,
			)
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return &Conversation{ID: convID, Name: name, IsGroup: payload.IsGroup}, nil
}

func getUserConversations(userID int) ([]Conversation, error) {
	rows, err := db.Query(`
		SELECT 
			c.id, 
			c.name, 
			c.is_group, 
			c.created_at,
			(SELECT COUNT(*) FROM messages m 
			 WHERE m.conversation_id = c.id 
			 AND m.created_at > cp.last_read_at) as unread_count
		FROM conversations c
		JOIN conversation_participants cp ON c.id = cp.conversation_id
		WHERE cp.user_id = $1
		ORDER BY c.created_at DESC
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var convs []Conversation
	for rows.Next() {
		var c Conversation
		if err := rows.Scan(&c.ID, &c.Name, &c.IsGroup, &c.CreatedAt, &c.UnreadCount); err != nil {
			continue
		}
		convs = append(convs, c)
	}
	return convs, nil
}

func addParticipant(convID int, username string) error {
	exists, userID := checkUserExists(username)
	if !exists {
		return fmt.Errorf("user %s not found", username)
	}
	_, err := db.Exec(
		"INSERT INTO conversation_participants (conversation_id, user_id) VALUES ($1, $2) ON CONFLICT DO NOTHING",
		convID, userID,
	)
	return err
}

func renameConversation(convID int, newName string) error {
	_, err := db.Exec("UPDATE conversations SET name = $1 WHERE id = $2", newName, convID)
	return err
}

func leaveConversation(userID, convID int) error {
	_, err := db.Exec("DELETE FROM conversation_participants WHERE user_id = $1 AND conversation_id = $2", userID, convID)
	return err
}

func updateReadReceipt(userID, conversationID int) error {
	_, err := db.Exec(`
		UPDATE conversation_participants
		SET last_read_at = NOW()
		WHERE user_id = $1 AND conversation_id = $2
	`, userID, conversationID)
	return err
}

func getConversationMessages(convID int, limit int) ([]Message, error) {
	rows, err := db.Query(`
		SELECT m.id, m.conversation_id, m.sender_id, u.username, m.content, m.created_at
		FROM messages m
		LEFT JOIN users u ON m.sender_id = u.id
		WHERE m.conversation_id = $1
		ORDER BY m.created_at DESC
		LIMIT $2
	`, convID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var msgs []Message
	for rows.Next() {
		var m Message
		var senderUsername sql.NullString
		if err := rows.Scan(&m.ID, &m.ConversationID, &m.SenderID, &senderUsername, &m.Content, &m.CreatedAt); err != nil {
			continue
		}
		if senderUsername.Valid {
			m.SenderUsername = senderUsername.String
		}
		msgs = append(msgs, m)
	}

	// Reverse to get oldest first
	for i, j := 0, len(msgs)-1; i < j; i, j = i+1, j-1 {
		msgs[i], msgs[j] = msgs[j], msgs[i]
	}
	return msgs, nil
}

func saveMessage(convID, senderID int, content string) (*Message, error) {
	var msg Message
	err := db.QueryRow(`
		INSERT INTO messages (conversation_id, sender_id, content)
		VALUES ($1, $2, $3)
		RETURNING id, conversation_id, sender_id, content, created_at
	`, convID, senderID, content).Scan(&msg.ID, &msg.ConversationID, &msg.SenderID, &msg.Content, &msg.CreatedAt)
	if err != nil {
		return nil, err
	}

	// Get sender username
	db.QueryRow("SELECT username FROM users WHERE id = $1", senderID).Scan(&msg.SenderUsername)
	return &msg, nil
}

// --- WebSocket Handler ---

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

func handleWebSocket(hub *Hub, w http.ResponseWriter, r *http.Request) {
	clientIP := getClientIP(r)

	// Rate limit: check connection count per IP
	if !rateLimiter.canConnect(clientIP) {
		http.Error(w, "Too many connections from your IP", http.StatusTooManyRequests)
		log.Printf("Rate limited connection from %s", clientIP)
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("Upgrade error:", err)
		return
	}

	rateLimiter.addConnection(clientIP)

	client := &Client{
		conn: conn,
		send: make(chan []byte, 256),
	}

	// Writer goroutine
	go func() {
		defer func() {
			rateLimiter.removeConnection(clientIP)
			conn.Close()
		}()
		for msg := range client.send {
			if err := conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				return
			}
		}
	}()

	// Reader loop
	for {
		_, msgBytes, err := conn.ReadMessage()
		if err != nil {
			if client.userID != 0 {
				hub.unregister <- client
			}
			return
		}

		log.Printf("Received message: %s", string(msgBytes)) // DEBUG

		var wsMsg WSMessage
		if err := json.Unmarshal(msgBytes, &wsMsg); err != nil {
			log.Printf("JSON Unmarshal error: %v", err) // DEBUG
			continue
		}

		log.Printf("Processing message type: %s", wsMsg.Type) // DEBUG

		switch wsMsg.Type {
		case "auth":
			// Rate limit auth attempts
			if !rateLimiter.canAuth(clientIP) {
				log.Printf("Rate limit hit for %s", clientIP) // DEBUG
				resp, _ := json.Marshal(map[string]interface{}{
					"type":  "auth_error",
					"error": "Too many login attempts. Please wait a minute.",
				})
				client.send <- resp
				continue
			}

			var payload AuthPayload
			json.Unmarshal(wsMsg.Payload, &payload)
			log.Printf("Auth attempt: Action=%s User=%s", payload.Action, payload.Username) // DEBUG

			userID, username, err := handleAuth(payload)
			if err != nil {
				log.Printf("Auth failed: %v", err) // DEBUG
				resp, _ := json.Marshal(map[string]interface{}{
					"type":  "auth_error",
					"error": err.Error(),
				})
				client.send <- resp
				continue
			}

			log.Printf("Auth success: UserID=%d User=%s", userID, username) // DEBUG

			client.userID = userID
			client.username = username
			hub.register <- client

			// Send success + conversations
			convs, _ := getUserConversations(userID)
			resp, _ := json.Marshal(map[string]interface{}{
				"type":          "auth_success",
				"user_id":       userID,
				"username":      username,
				"conversations": convs,
			})
			client.send <- resp

		case "typing":
			if client.userID == 0 {
				continue
			}
			var payload struct {
				ConversationID int `json:"conversation_id"`
			}
			json.Unmarshal(wsMsg.Payload, &payload)

			resp, _ := json.Marshal(map[string]interface{}{
				"type":            "typing",
				"conversation_id": payload.ConversationID,
				"user_id":         client.userID,
				"username":        client.username,
			})
			hub.broadcast <- resp

		case "check_user":
			var payload CheckUserPayload
			json.Unmarshal(wsMsg.Payload, &payload)
			exists, _ := checkUserExists(payload.Username)
			resp, _ := json.Marshal(map[string]interface{}{
				"type":     "user_check_result",
				"username": payload.Username,
				"exists":   exists,
			})
			client.send <- resp

		case "create_conversation":
			if client.userID == 0 {
				continue
			}
			var payload CreateConversationPayload
			json.Unmarshal(wsMsg.Payload, &payload)
			conv, err := createConversation(client.userID, payload)
			if err != nil {
				resp, _ := json.Marshal(map[string]interface{}{
					"type":  "error",
					"error": err.Error(),
				})
				client.send <- resp
				continue
			}
			resp, _ := json.Marshal(map[string]interface{}{
				"type":         "conversation_created",
				"conversation": conv,
			})
			client.send <- resp

		case "get_messages":
			if client.userID == 0 {
				continue
			}
			var payload struct {
				ConversationID int `json:"conversation_id"`
			}
			json.Unmarshal(wsMsg.Payload, &payload)

			// Mark as read when fetching messages
			updateReadReceipt(client.userID, payload.ConversationID)

			msgs, _ := getConversationMessages(payload.ConversationID, 100)
			resp, _ := json.Marshal(map[string]interface{}{
				"type":            "messages",
				"conversation_id": payload.ConversationID,
				"messages":        msgs,
			})
			client.send <- resp

		case "read_receipt":
			if client.userID == 0 {
				continue
			}
			var payload ReadReceiptPayload
			json.Unmarshal(wsMsg.Payload, &payload)
			updateReadReceipt(client.userID, payload.ConversationID)

		case "send_message":
			if client.userID == 0 {
				continue
			}
			var payload SendMessagePayload
			json.Unmarshal(wsMsg.Payload, &payload)
			msg, err := saveMessage(payload.ConversationID, client.userID, payload.Content)
			if err != nil {
				continue
			}

			// Broadcast to all connected clients
			resp, _ := json.Marshal(map[string]interface{}{
				"type":    "new_message",
				"message": msg,
			})
			hub.broadcast <- resp

		case "get_conversations":
			if client.userID == 0 {
				continue
			}
			convs, _ := getUserConversations(client.userID)
			resp, _ := json.Marshal(map[string]interface{}{
				"type":          "conversations",
				"conversations": convs,
			})
			client.send <- resp

		case "add_participant":
			if client.userID == 0 {
				continue
			}
			var payload struct {
				ConversationID int    `json:"conversation_id"`
				Username       string `json:"username"`
			}
			json.Unmarshal(wsMsg.Payload, &payload)
			err := addParticipant(payload.ConversationID, payload.Username)
			if err != nil {
				resp, _ := json.Marshal(map[string]interface{}{
					"type": "error", "error": err.Error(),
				})
				client.send <- resp
				continue
			}
			// Refresh conversations for the client
			convs, _ := getUserConversations(client.userID)
			resp, _ := json.Marshal(map[string]interface{}{
				"type": "conversations", "conversations": convs,
			})
			client.send <- resp

		case "rename_conversation":
			if client.userID == 0 {
				continue
			}
			var payload struct {
				ConversationID int    `json:"conversation_id"`
				Name           string `json:"name"`
			}
			json.Unmarshal(wsMsg.Payload, &payload)
			renameConversation(payload.ConversationID, payload.Name)
			// Refresh
			convs, _ := getUserConversations(client.userID)
			resp, _ := json.Marshal(map[string]interface{}{
				"type": "conversations", "conversations": convs,
			})
			client.send <- resp

		case "leave_conversation":
			if client.userID == 0 {
				continue
			}
			var payload struct {
				ConversationID int `json:"conversation_id"`
			}
			json.Unmarshal(wsMsg.Payload, &payload)
			leaveConversation(client.userID, payload.ConversationID)
			// Refresh
			convs, _ := getUserConversations(client.userID)
			resp, _ := json.Marshal(map[string]interface{}{
				"type": "conversations", "conversations": convs,
			})
			client.send <- resp
		}
	}
}

// --- Main ---

func main() {
	initDB()
	rateLimiter = newRateLimiter()

	hub := newHub()
	go hub.run()

	http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		handleWebSocket(hub, w, r)
	})

	// Health check
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	port := os.Getenv("PORT")
	if port == "" {
		port = "3567"
	}

	log.Printf("Server starting on :%s", port)
	log.Printf("Rate limits: %d connections/IP, %d auth attempts/min", rateLimiter.maxConns, rateLimiter.maxAuth)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}
