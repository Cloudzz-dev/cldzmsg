package models

import (
	"encoding/json"
	"time"
)

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
	UnreadCount  int       `json:"unread_count"`
}

// WS Message Types

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

type ReadReceiptPayload struct {
	ConversationID int `json:"conversation_id"`
}
