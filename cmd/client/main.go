package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/gorilla/websocket"
)

// --- Styles ---

var (
	primaryColor   = lipgloss.Color("#7C3AED")
	secondaryColor = lipgloss.Color("#10B981")
	bgColor        = lipgloss.Color("#1F2937")
	textColor      = lipgloss.Color("#F9FAFB")
	mutedColor     = lipgloss.Color("#9CA3AF")
	errorColor     = lipgloss.Color("#EF4444")

	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(primaryColor).
			Padding(0, 1)

	boxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(primaryColor).
			Padding(1, 2)

	selectedStyle = lipgloss.NewStyle().
			Foreground(secondaryColor).
			Bold(true)

	mutedStyle = lipgloss.NewStyle().
			Foreground(mutedColor)

	errorStyle = lipgloss.NewStyle().
			Foreground(errorColor).
			Bold(true)

	messageStyle = lipgloss.NewStyle().
			Padding(0, 1)

	ownMessageStyle = lipgloss.NewStyle().
			Foreground(secondaryColor)

	otherMessageStyle = lipgloss.NewStyle().
				Foreground(primaryColor)

	helpStyle = lipgloss.NewStyle().
			Foreground(mutedColor).
			Italic(true)
)

// --- View State ---

type viewState int

const (
	viewAuth viewState = iota
	viewConversations
	viewChat
	viewNewConversation
)

// --- Models ---

type Message struct {
	ID             int       `json:"id"`
	ConversationID int       `json:"conversation_id"`
	SenderID       int       `json:"sender_id"`
	SenderUsername string    `json:"sender_username"`
	Content        string    `json:"content"`
	CreatedAt      time.Time `json:"created_at"`
}

type Conversation struct {
	ID        int       `json:"id"`
	Name      *string   `json:"name"`
	IsGroup   bool      `json:"is_group"`
	CreatedAt time.Time `json:"created_at"`
}

// --- WebSocket Messages ---

type wsMessage struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

type wsIncoming struct {
	data []byte
}

type wsError struct {
	err error
}

type wsConnected struct {
	conn *websocket.Conn
}

// --- Main Model ---

type model struct {
	// Connection
	conn      *websocket.Conn
	serverURL string
	connected bool

	// Auth
	userID       int
	username     string
	authAction   string // "login" or "register"
	usernameInput textinput.Model
	passwordInput textinput.Model
	authFocused   int // 0=username, 1=password
	authError     string

	// Conversations
	conversations    []Conversation
	selectedConv     int
	currentConvID    int
	currentConvName  string

	// Messages
	messages      []Message
	messageInput  textinput.Model
	chatViewport  viewport.Model

	// New conversation
	newConvInput  textinput.Model
	newConvIsGroup bool
	newConvUsers  []string

	// UI
	view   viewState
	width  int
	height int
	err    error
}

func initialModel(serverURL string) model {
	usernameInput := textinput.New()
	usernameInput.Placeholder = "Username"
	usernameInput.Focus()
	usernameInput.CharLimit = 32
	usernameInput.Width = 30

	passwordInput := textinput.New()
	passwordInput.Placeholder = "Password"
	passwordInput.EchoMode = textinput.EchoPassword
	passwordInput.CharLimit = 64
	passwordInput.Width = 30

	messageInput := textinput.New()
	messageInput.Placeholder = "Type a message..."
	messageInput.CharLimit = 1000
	messageInput.Width = 50

	newConvInput := textinput.New()
	newConvInput.Placeholder = "Enter username to add..."
	newConvInput.CharLimit = 32
	newConvInput.Width = 30

	chatViewport := viewport.New(80, 20)

	return model{
		serverURL:     serverURL,
		authAction:    "login",
		usernameInput: usernameInput,
		passwordInput: passwordInput,
		messageInput:  messageInput,
		newConvInput:  newConvInput,
		chatViewport:  chatViewport,
		view:          viewAuth,
	}
}

// --- Commands ---

func connectToServer(url string) tea.Cmd {
	return func() tea.Msg {
		conn, _, err := websocket.DefaultDialer.Dial(url, nil)
		if err != nil {
			return wsError{err: err}
		}
		return wsConnected{conn: conn}
	}
}

func listenForMessages(conn *websocket.Conn) tea.Cmd {
	return func() tea.Msg {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			return wsError{err: err}
		}
		return wsIncoming{data: msg}
	}
}

func (m model) sendWSMessage(msgType string, payload interface{}) tea.Cmd {
	return func() tea.Msg {
		if m.conn == nil {
			return nil
		}

		payloadBytes, _ := json.Marshal(payload)
		msg := wsMessage{
			Type:    msgType,
			Payload: payloadBytes,
		}
		msgBytes, _ := json.Marshal(msg)
		m.conn.WriteMessage(websocket.TextMessage, msgBytes)
		return nil
	}
}

// --- Init ---

func (m model) Init() tea.Cmd {
	return tea.Batch(
		textinput.Blink,
		connectToServer(m.serverURL),
	)
}

// --- Update ---

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			if m.view == viewAuth {
				return m, tea.Quit
			}
			if m.view == viewConversations {
				return m, tea.Quit
			}
			// Go back from chat
			if m.view == viewChat {
				m.view = viewConversations
				return m, nil
			}
			if m.view == viewNewConversation {
				m.view = viewConversations
				return m, nil
			}

		case "tab":
			if m.view == viewAuth {
				if m.authFocused == 0 {
					m.authFocused = 1
					m.usernameInput.Blur()
					m.passwordInput.Focus()
				} else {
					m.authFocused = 0
					m.passwordInput.Blur()
					m.usernameInput.Focus()
				}
			}

		case "ctrl+r":
			if m.view == viewAuth {
				if m.authAction == "login" {
					m.authAction = "register"
				} else {
					m.authAction = "login"
				}
			}

		case "enter":
			switch m.view {
			case viewAuth:
				if m.usernameInput.Value() != "" && m.passwordInput.Value() != "" {
					return m, m.sendWSMessage("auth", map[string]string{
						"username": m.usernameInput.Value(),
						"password": m.passwordInput.Value(),
						"action":   m.authAction,
					})
				}

			case viewConversations:
				if len(m.conversations) > 0 {
					conv := m.conversations[m.selectedConv]
					m.currentConvID = conv.ID
					if conv.Name != nil {
						m.currentConvName = *conv.Name
					} else {
						m.currentConvName = fmt.Sprintf("DM #%d", conv.ID)
					}
					m.view = viewChat
					m.messageInput.Focus()
					return m, m.sendWSMessage("get_messages", map[string]int{
						"conversation_id": conv.ID,
					})
				}

			case viewChat:
				if m.messageInput.Value() != "" {
					content := m.messageInput.Value()
					m.messageInput.SetValue("")
					return m, m.sendWSMessage("send_message", map[string]interface{}{
						"conversation_id": m.currentConvID,
						"content":         content,
					})
				}

			case viewNewConversation:
				if m.newConvInput.Value() != "" {
					username := m.newConvInput.Value()
					m.newConvInput.SetValue("")
					m.newConvUsers = append(m.newConvUsers, username)
				}
			}

		case "up", "k":
			if m.view == viewConversations && m.selectedConv > 0 {
				m.selectedConv--
			}

		case "down", "j":
			if m.view == viewConversations && m.selectedConv < len(m.conversations)-1 {
				m.selectedConv++
			}

		case "n":
			if m.view == viewConversations {
				m.view = viewNewConversation
				m.newConvInput.Focus()
				m.newConvUsers = []string{}
			}

		case "ctrl+g":
			if m.view == viewNewConversation {
				m.newConvIsGroup = !m.newConvIsGroup
			}

		case "ctrl+s":
			if m.view == viewNewConversation && len(m.newConvUsers) > 0 {
				var name string
				if m.newConvIsGroup {
					name = fmt.Sprintf("Group: %s", strings.Join(m.newConvUsers, ", "))
				}
				m.view = viewConversations
				return m, m.sendWSMessage("create_conversation", map[string]interface{}{
					"name":      name,
					"is_group":  m.newConvIsGroup,
					"usernames": m.newConvUsers,
				})
			}

		case "esc":
			if m.view == viewChat {
				m.view = viewConversations
			}
			if m.view == viewNewConversation {
				m.view = viewConversations
			}
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.chatViewport.Width = msg.Width - 4
		m.chatViewport.Height = msg.Height - 8

	case wsConnected:
		m.conn = msg.conn
		m.connected = true
		return m, listenForMessages(m.conn)

	case wsError:
		m.err = msg.err
		return m, nil

	case wsIncoming:
		var wsMsg struct {
			Type string `json:"type"`
		}
		json.Unmarshal(msg.data, &wsMsg)

		switch wsMsg.Type {
		case "auth_success":
			var resp struct {
				UserID        int            `json:"user_id"`
				Username      string         `json:"username"`
				Conversations []Conversation `json:"conversations"`
			}
			json.Unmarshal(msg.data, &resp)
			m.userID = resp.UserID
			m.username = resp.Username
			m.conversations = resp.Conversations
			m.view = viewConversations
			m.authError = ""

		case "auth_error":
			var resp struct {
				Error string `json:"error"`
			}
			json.Unmarshal(msg.data, &resp)
			m.authError = resp.Error

		case "conversations":
			var resp struct {
				Conversations []Conversation `json:"conversations"`
			}
			json.Unmarshal(msg.data, &resp)
			m.conversations = resp.Conversations

		case "conversation_created":
			var resp struct {
				Conversation Conversation `json:"conversation"`
			}
			json.Unmarshal(msg.data, &resp)
			m.conversations = append([]Conversation{resp.Conversation}, m.conversations...)

		case "messages":
			var resp struct {
				Messages []Message `json:"messages"`
			}
			json.Unmarshal(msg.data, &resp)
			m.messages = resp.Messages
			m.updateChatViewport()

		case "new_message":
			var resp struct {
				Message Message `json:"message"`
			}
			json.Unmarshal(msg.data, &resp)
			if resp.Message.ConversationID == m.currentConvID {
				m.messages = append(m.messages, resp.Message)
				m.updateChatViewport()
			}
		}

		if m.conn != nil {
			cmds = append(cmds, listenForMessages(m.conn))
		}
	}

	// Update text inputs
	switch m.view {
	case viewAuth:
		if m.authFocused == 0 {
			m.usernameInput, _ = m.usernameInput.Update(msg)
		} else {
			m.passwordInput, _ = m.passwordInput.Update(msg)
		}
	case viewChat:
		m.messageInput, _ = m.messageInput.Update(msg)
		m.chatViewport, _ = m.chatViewport.Update(msg)
	case viewNewConversation:
		m.newConvInput, _ = m.newConvInput.Update(msg)
	}

	return m, tea.Batch(cmds...)
}

func (m *model) updateChatViewport() {
	var content strings.Builder
	for _, msg := range m.messages {
		timestamp := msg.CreatedAt.Format("15:04")
		var style lipgloss.Style
		if msg.SenderID == m.userID {
			style = ownMessageStyle
		} else {
			style = otherMessageStyle
		}
		line := fmt.Sprintf("%s %s: %s",
			mutedStyle.Render(timestamp),
			style.Render(msg.SenderUsername),
			msg.Content,
		)
		content.WriteString(line + "\n")
	}
	m.chatViewport.SetContent(content.String())
	m.chatViewport.GotoBottom()
}

// --- View ---

func (m model) View() string {
	if m.err != nil {
		return errorStyle.Render(fmt.Sprintf("Error: %v\n\nPress q to quit.", m.err))
	}

	switch m.view {
	case viewAuth:
		return m.authView()
	case viewConversations:
		return m.conversationsView()
	case viewChat:
		return m.chatView()
	case viewNewConversation:
		return m.newConversationView()
	}
	return ""
}

func (m model) authView() string {
	var s strings.Builder

	title := titleStyle.Render("╔═══════════════════════════════╗\n║         CLDZMSG               ║\n╚═══════════════════════════════╝")

	s.WriteString("\n\n")
	s.WriteString(title)
	s.WriteString("\n\n")

	action := m.authAction
	if action == "login" {
		s.WriteString(selectedStyle.Render("  → Login"))
		s.WriteString(mutedStyle.Render("   Register\n"))
	} else {
		s.WriteString(mutedStyle.Render("  Login   "))
		s.WriteString(selectedStyle.Render("→ Register\n"))
	}
	s.WriteString(helpStyle.Render("  (Ctrl+R to switch)\n\n"))

	s.WriteString("  Username:\n")
	s.WriteString("  " + m.usernameInput.View() + "\n\n")
	s.WriteString("  Password:\n")
	s.WriteString("  " + m.passwordInput.View() + "\n\n")

	if m.authError != "" {
		s.WriteString(errorStyle.Render("  " + m.authError + "\n\n"))
	}

	s.WriteString(helpStyle.Render("  Tab to switch fields • Enter to submit • q to quit\n"))

	if !m.connected {
		s.WriteString(mutedStyle.Render("\n  Connecting to server..."))
	}

	return s.String()
}

func (m model) conversationsView() string {
	var s strings.Builder

	s.WriteString(titleStyle.Render(fmt.Sprintf("CLDZMSG - %s", m.username)))
	s.WriteString("\n\n")

	if len(m.conversations) == 0 {
		s.WriteString(mutedStyle.Render("  No conversations yet.\n"))
		s.WriteString(mutedStyle.Render("  Press 'n' to start a new one.\n"))
	} else {
		for i, conv := range m.conversations {
			var name string
			if conv.Name != nil {
				name = *conv.Name
			} else {
				name = fmt.Sprintf("DM #%d", conv.ID)
			}

			prefix := "  "
			style := lipgloss.NewStyle()
			if i == m.selectedConv {
				prefix = "→ "
				style = selectedStyle
			}

			icon := "💬"
			if conv.IsGroup {
				icon = "👥"
			}

			s.WriteString(style.Render(fmt.Sprintf("%s%s %s\n", prefix, icon, name)))
		}
	}

	s.WriteString("\n")
	s.WriteString(helpStyle.Render("  ↑/↓ navigate • Enter to open • n for new • q to quit"))

	return s.String()
}

func (m model) chatView() string {
	var s strings.Builder

	header := titleStyle.Render(fmt.Sprintf("💬 %s", m.currentConvName))
	s.WriteString(header)
	s.WriteString("\n")
	s.WriteString(strings.Repeat("─", m.width-2))
	s.WriteString("\n")

	s.WriteString(m.chatViewport.View())
	s.WriteString("\n")
	s.WriteString(strings.Repeat("─", m.width-2))
	s.WriteString("\n")
	s.WriteString(m.messageInput.View())
	s.WriteString("\n")
	s.WriteString(helpStyle.Render("Enter to send • Esc to go back"))

	return s.String()
}

func (m model) newConversationView() string {
	var s strings.Builder

	s.WriteString(titleStyle.Render("New Conversation"))
	s.WriteString("\n\n")

	groupLabel := "Direct Message"
	if m.newConvIsGroup {
		groupLabel = "Group Chat"
	}
	s.WriteString(fmt.Sprintf("  Type: %s\n", selectedStyle.Render(groupLabel)))
	s.WriteString(helpStyle.Render("  (Ctrl+G to toggle)\n\n"))

	s.WriteString("  Add users:\n")
	s.WriteString("  " + m.newConvInput.View() + "\n\n")

	if len(m.newConvUsers) > 0 {
		s.WriteString("  Added:\n")
		for _, u := range m.newConvUsers {
			s.WriteString(fmt.Sprintf("    • %s\n", u))
		}
	}

	s.WriteString("\n")
	s.WriteString(helpStyle.Render("  Enter to add user • Ctrl+S to create • Esc to cancel"))

	return s.String()
}

// --- Main ---

func main() {
	serverURL := os.Getenv("CLDZMSG_SERVER")
	if serverURL == "" {
		serverURL = "ws://localhost:8080/ws"
	}

	p := tea.NewProgram(initialModel(serverURL), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}
}
