package storage

import (
	"database/sql"
	"fmt"
	"log"
	"os"

	"github.com/cloudzz-dev/cldzmsg/internal/server/models"
	_ "github.com/lib/pq"
)

type Store struct {
	db *sql.DB
}

func New() *Store {
	connStr := os.Getenv("DATABASE_URL")
	if connStr == "" {
		connStr = "postgres://localhost/cldzmsg?sslmode=disable"
	}

	db, err := sql.Open("postgres", connStr)
	if err != nil {
		log.Fatal("Failed to connect to database:", err)
	}

	if err = db.Ping(); err != nil {
		log.Fatal("Failed to ping database:", err)
	}

	log.Println("Connected to database")
	return &Store{db: db}
}

func (s *Store) Close() {
	s.db.Close()
}

// User Methods

func (s *Store) CreateUser(username, passwordHash string) (int, error) {
	var userID int
	err := s.db.QueryRow(
		"INSERT INTO users (username, password_hash) VALUES ($1, $2) RETURNING id",
		username, passwordHash,
	).Scan(&userID)
	return userID, err
}

func (s *Store) GetUserByUsername(username string) (*models.User, error) {
	var u models.User
	err := s.db.QueryRow(
		"SELECT id, username, password_hash FROM users WHERE username = $1",
		username,
	).Scan(&u.ID, &u.Username, &u.PasswordHash)
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func (s *Store) GetUserByID(id int) (*models.User, error) {
	var u models.User
	err := s.db.QueryRow(
		"SELECT id, username FROM users WHERE id = $1",
		id,
	).Scan(&u.ID, &u.Username)
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func (s *Store) CheckUserExists(username string) (bool, int) {
	var userID int
	err := s.db.QueryRow("SELECT id FROM users WHERE username = $1", username).Scan(&userID)
	if err != nil {
		return false, 0
	}
	return true, userID
}

// Conversation Methods

func (s *Store) CreateConversation(creatorID int, payload models.CreateConversationPayload) (*models.Conversation, error) {
	tx, err := s.db.Begin()
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
		exists, userID := s.CheckUserExists(username)
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

	// After commit, if it's a DM and name is null, fetch the other participant's name
	finalName := name
	if !payload.IsGroup && name == nil && len(payload.Usernames) > 0 {
		n := payload.Usernames[0]
		finalName = &n
	}

	return &models.Conversation{ID: convID, Name: finalName, IsGroup: payload.IsGroup}, nil
}

func (s *Store) GetUserConversations(userID int) ([]models.Conversation, error) {
	rows, err := s.db.Query(`
		SELECT 
			c.id, 
			COALESCE(c.name, (
				SELECT u.username 
				FROM conversation_participants cp2 
				JOIN users u ON cp2.user_id = u.id 
				WHERE cp2.conversation_id = c.id AND cp2.user_id != $1 
				LIMIT 1
			)) as name, 
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

	var convs []models.Conversation
	for rows.Next() {
		var c models.Conversation
		if err := rows.Scan(&c.ID, &c.Name, &c.IsGroup, &c.CreatedAt, &c.UnreadCount); err != nil {
			log.Printf("Error scanning conversation: %v", err)
			continue
		}
		convs = append(convs, c)
	}
	return convs, nil
}

func (s *Store) AddParticipant(convID int, username string) error {
	exists, userID := s.CheckUserExists(username)
	if !exists {
		return fmt.Errorf("user %s not found", username)
	}
	_, err := s.db.Exec(
		"INSERT INTO conversation_participants (conversation_id, user_id) VALUES ($1, $2) ON CONFLICT DO NOTHING",
		convID, userID,
	)
	return err
}

func (s *Store) RenameConversation(convID int, newName string) error {
	var name *string
	if newName != "" {
		name = &newName
	}
	_, err := s.db.Exec("UPDATE conversations SET name = $1 WHERE id = $2", name, convID)
	return err
}

func (s *Store) LeaveConversation(userID, convID int) error {
	_, err := s.db.Exec("DELETE FROM conversation_participants WHERE user_id = $1 AND conversation_id = $2", userID, convID)
	return err
}

func (s *Store) UpdateReadReceipt(userID, conversationID int) error {
	_, err := s.db.Exec(`
		UPDATE conversation_participants
		SET last_read_at = NOW()
		WHERE user_id = $1 AND conversation_id = $2
	`, userID, conversationID)
	return err
}

// Message Methods

func (s *Store) GetConversationMessages(convID int, limit int) ([]models.Message, error) {
	rows, err := s.db.Query(`
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

	var msgs []models.Message
	for rows.Next() {
		var m models.Message
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

func (s *Store) SaveMessage(convID, senderID int, content string) (*models.Message, error) {
	var msg models.Message
	err := s.db.QueryRow(`
		INSERT INTO messages (conversation_id, sender_id, content)
		VALUES ($1, $2, $3)
		RETURNING id, conversation_id, sender_id, content, created_at
	`, convID, senderID, content).Scan(&msg.ID, &msg.ConversationID, &msg.SenderID, &msg.Content, &msg.CreatedAt)
	if err != nil {
		return nil, err
	}

	// Get sender username
	// Optimization: we could pass the username to avoid a query, but this is safer
	user, err := s.GetUserByID(senderID)
	if err == nil {
		msg.SenderUsername = user.Username
	}
	return &msg, nil
}
