package main

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	_ "github.com/lib/pq"
	"golang.org/x/crypto/bcrypt"
)

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

type Conversation struct {
	ID           int       `json:"id"`
	Name         *string   `json:"name,omitempty"`
	IsGroup      bool      `json:"is_group"`
	CreatedAt    time.Time `json:"created_at"`
	Participants []string  `json:"participants,omitempty"`
}

// --- WebSocket Hub ---

type Client struct {
	conn   *websocket.Conn
	userID int
	send   chan []byte
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
	Name        string   `json:"name,omitempty"`
	IsGroup     bool     `json:"is_group"`
	Usernames   []string `json:"usernames"`
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
		SELECT c.id, c.name, c.is_group, c.created_at
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
		if err := rows.Scan(&c.ID, &c.Name, &c.IsGroup, &c.CreatedAt); err != nil {
			continue
		}
		convs = append(convs, c)
	}
	return convs, nil
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
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("Upgrade error:", err)
		return
	}

	client := &Client{
		conn: conn,
		send: make(chan []byte, 256),
	}

	// Writer goroutine
	go func() {
		defer conn.Close()
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

		var wsMsg WSMessage
		if err := json.Unmarshal(msgBytes, &wsMsg); err != nil {
			continue
		}

		switch wsMsg.Type {
		case "auth":
			var payload AuthPayload
			json.Unmarshal(wsMsg.Payload, &payload)

			userID, username, err := handleAuth(payload)
			if err != nil {
				resp, _ := json.Marshal(map[string]interface{}{
					"type":  "auth_error",
					"error": err.Error(),
				})
				client.send <- resp
				continue
			}

			client.userID = userID
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
			msgs, _ := getConversationMessages(payload.ConversationID, 100)
			resp, _ := json.Marshal(map[string]interface{}{
				"type":            "messages",
				"conversation_id": payload.ConversationID,
				"messages":        msgs,
			})
			client.send <- resp

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
		}
	}
}

// --- Main ---

func main() {
	initDB()

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
		port = "8080"
	}

	log.Printf("Server starting on :%s", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}
