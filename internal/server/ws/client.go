package ws

import (
	"encoding/json"
	"log"

	"github.com/cloudzz-dev/cldzmsg/internal/server/models"
	"github.com/cloudzz-dev/cldzmsg/internal/server/ratelimit"
	"github.com/gorilla/websocket"
	"golang.org/x/crypto/bcrypt"
)

type Client struct {
	Hub      *Hub
	Conn     *websocket.Conn
	Send     chan []byte
	UserID   int
	Username string
	IP       string
	Limiter  *ratelimit.RateLimiter
}

func (c *Client) ReadPump() {
	defer func() {
		c.Hub.Unregister <- c
		c.Conn.Close()
	}()

	for {
		_, msgBytes, err := c.Conn.ReadMessage()
		if err != nil {
			break
		}

		var wsMsg models.WSMessage
		if err := json.Unmarshal(msgBytes, &wsMsg); err != nil {
			log.Printf("JSON Unmarshal error: %v", err)
			continue
		}

		c.ProcessMessage(wsMsg)
	}
}

func (c *Client) WritePump() {
	defer func() {
		c.Conn.Close()
	}()
	for msg := range c.Send {
		c.Conn.WriteMessage(websocket.TextMessage, msg)
	}
}

func (c *Client) ProcessMessage(msg models.WSMessage) {
	switch msg.Type {
	case "auth":
		if !c.Limiter.CanAuth(c.IP) {
			c.SendError("auth_error", "Too many login attempts. Please wait a minute.")
			return
		}

		var payload models.AuthPayload
		json.Unmarshal(msg.Payload, &payload)

		userID, username, err := c.handleAuth(payload)
		if err != nil {
			c.SendError("auth_error", err.Error())
			return
		}

		c.UserID = userID
		c.Username = username
		c.Hub.Register <- c

		convs, _ := c.Hub.Store.GetUserConversations(userID)
		c.SendJSON(map[string]interface{}{
			"type":          "auth_success",
			"user_id":       userID,
			"username":      username,
			"conversations": convs,
		})

	case "typing":
		if c.UserID == 0 {
			return
		}
		var payload struct {
			ConversationID int `json:"conversation_id"`
		}
		json.Unmarshal(msg.Payload, &payload)

		c.Hub.Broadcast <- c.MarshalJSON(map[string]interface{}{
			"type":            "typing",
			"conversation_id": payload.ConversationID,
			"user_id":         c.UserID,
			"username":        c.Username,
		})

	case "check_user":
		var payload models.CheckUserPayload
		json.Unmarshal(msg.Payload, &payload)
		exists, _ := c.Hub.Store.CheckUserExists(payload.Username)
		c.SendJSON(map[string]interface{}{
			"type":     "user_check_result",
			"username": payload.Username,
			"exists":   exists,
		})

	case "create_conversation":
		if c.UserID == 0 {
			return
		}
		var payload models.CreateConversationPayload
		json.Unmarshal(msg.Payload, &payload)
		conv, err := c.Hub.Store.CreateConversation(c.UserID, payload)
		if err != nil {
			c.SendError("error", err.Error())
			return
		}
		c.SendJSON(map[string]interface{}{
			"type":         "conversation_created",
			"conversation": conv,
		})

	case "get_messages":
		if c.UserID == 0 {
			return
		}
		var payload struct {
			ConversationID int `json:"conversation_id"`
		}
		json.Unmarshal(msg.Payload, &payload)

		c.Hub.Store.UpdateReadReceipt(c.UserID, payload.ConversationID)

		msgs, _ := c.Hub.Store.GetConversationMessages(payload.ConversationID, 100)
		c.SendJSON(map[string]interface{}{
			"type":            "messages",
			"conversation_id": payload.ConversationID,
			"messages":        msgs,
		})

	case "read_receipt":
		if c.UserID == 0 {
			return
		}
		var payload models.ReadReceiptPayload
		json.Unmarshal(msg.Payload, &payload)
		c.Hub.Store.UpdateReadReceipt(c.UserID, payload.ConversationID)

	case "send_message":
		if c.UserID == 0 {
			return
		}
		var payload models.SendMessagePayload
		json.Unmarshal(msg.Payload, &payload)
		msg, err := c.Hub.Store.SaveMessage(payload.ConversationID, c.UserID, payload.Content)
		if err != nil {
			return
		}

		c.Hub.Broadcast <- c.MarshalJSON(map[string]interface{}{
			"type":    "new_message",
			"message": msg,
		})

	case "get_conversations":
		if c.UserID == 0 {
			return
		}
		convs, _ := c.Hub.Store.GetUserConversations(c.UserID)
		c.SendJSON(map[string]interface{}{
			"type":          "conversations",
			"conversations": convs,
		})

	case "add_participant":
		if c.UserID == 0 {
			return
		}
		var payload struct {
			ConversationID int    `json:"conversation_id"`
			Username       string `json:"username"`
		}
		json.Unmarshal(msg.Payload, &payload)
		err := c.Hub.Store.AddParticipant(payload.ConversationID, payload.Username)
		if err != nil {
			c.SendError("error", err.Error())
			return
		}
		// Refresh
		convs, _ := c.Hub.Store.GetUserConversations(c.UserID)
		c.SendJSON(map[string]interface{}{
			"type": "conversations", "conversations": convs,
		})

	case "rename_conversation":
		if c.UserID == 0 {
			return
		}
		var payload struct {
			ConversationID int    `json:"conversation_id"`
			Name           string `json:"name"`
		}
		json.Unmarshal(msg.Payload, &payload)
		c.Hub.Store.RenameConversation(payload.ConversationID, payload.Name)
		// Refresh
		convs, _ := c.Hub.Store.GetUserConversations(c.UserID)
		c.SendJSON(map[string]interface{}{
			"type": "conversations", "conversations": convs,
		})

	case "leave_conversation":
		if c.UserID == 0 {
			return
		}
		var payload struct {
			ConversationID int `json:"conversation_id"`
		}
		json.Unmarshal(msg.Payload, &payload)
		c.Hub.Store.LeaveConversation(c.UserID, payload.ConversationID)
		// Refresh
		convs, _ := c.Hub.Store.GetUserConversations(c.UserID)
		c.SendJSON(map[string]interface{}{
			"type": "conversations", "conversations": convs,
		})
	}
}

func (c *Client) handleAuth(payload models.AuthPayload) (int, string, error) {
	if payload.Action == "register" {
		hash, err := bcrypt.GenerateFromPassword([]byte(payload.Password), bcrypt.DefaultCost)
		if err != nil {
			return 0, "", err
		}
		id, err := c.Hub.Store.CreateUser(payload.Username, string(hash))
		return id, payload.Username, err
	}

	// Login
	user, err := c.Hub.Store.GetUserByUsername(payload.Username)
	if err != nil {
		return 0, "", err
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(payload.Password)); err != nil {
		return 0, "", err
	}

	return user.ID, user.Username, nil
}

func (c *Client) SendJSON(v interface{}) {
	data, _ := json.Marshal(v)
	c.Send <- data
}

func (c *Client) SendError(typeStr, errStr string) {
	c.SendJSON(map[string]string{
		"type":  typeStr,
		"error": errStr,
	})
}

func (c *Client) MarshalJSON(v interface{}) []byte {
	data, _ := json.Marshal(v)
	return data
}
