package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/gorilla/websocket"
)

// --- Session Persistence ---

type Session struct {
	Username string `json:"username"`
	Password string `json:"password"` // Stored for auto-login (consider encrypting in production)
}

var profileName = "default"

func getConfigDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "cldzmsg", profileName)
}

func loadSession() *Session {
	configDir := getConfigDir()
	if configDir == "" {
		return nil
	}

	data, err := os.ReadFile(filepath.Join(configDir, "session.json"))
	if err != nil {
		return nil
	}

	var session Session
	if err := json.Unmarshal(data, &session); err != nil {
		return nil
	}
	return &session
}

func saveSession(username, password string) error {
	configDir := getConfigDir()
	if configDir == "" {
		return fmt.Errorf("could not get config directory")
	}

	if err := os.MkdirAll(configDir, 0700); err != nil {
		return err
	}

	session := Session{Username: username, Password: password}
	data, err := json.Marshal(session)
	if err != nil {
		return err
	}

	return os.WriteFile(filepath.Join(configDir, "session.json"), data, 0600)
}

func clearSession() {
	configDir := getConfigDir()
	if configDir != "" {
		os.Remove(filepath.Join(configDir, "session.json"))
	}
}

// --- Styles ---

var (
	primaryColor   = lipgloss.Color("#7C3AED")
	secondaryColor = lipgloss.Color("#10B981") // Green for self
	otherColor     = lipgloss.Color("#3B82F6") // Blue for others
	bgColor        = lipgloss.Color("#1F2937")
	textColor      = lipgloss.Color("#F9FAFB")
	mutedColor     = lipgloss.Color("#9CA3AF")
	errorColor     = lipgloss.Color("#EF4444")
	activeBorder   = lipgloss.Color("#F59E0B") // Amber for focus

	// App container
	appStyle = lipgloss.NewStyle().Padding(1, 2)

	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(primaryColor).
			Padding(0, 1)

	profileStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#34D399")). // Emerald
			Bold(true)

	// Utils
	mutedStyle = lipgloss.NewStyle().
			Foreground(mutedColor)

	errorStyle = lipgloss.NewStyle().
			Foreground(errorColor).
			Bold(true)

	boxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(primaryColor).
			Padding(1, 2)

	helpStyle = lipgloss.NewStyle().
			Foreground(mutedColor).
			Italic(true)

	// Sidebar styles
	sidebarStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(primaryColor).
			Padding(0, 1).
			MarginRight(1)

	selectedItemStyle = lipgloss.NewStyle().
				Foreground(secondaryColor).
				Bold(true).
				PaddingLeft(1).
				Border(lipgloss.NormalBorder(), false, false, false, true).
				BorderForeground(secondaryColor)

	unselectedItemStyle = lipgloss.NewStyle().
				PaddingLeft(2) // Match indentation of selected items

	// Chat styles
	chatWindowStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(primaryColor)

	headerStyle = lipgloss.NewStyle().
			Bold(true).
			Border(lipgloss.NormalBorder(), false, false, true, false).
			BorderForeground(mutedColor).
			Padding(0, 1).
			Width(100) // Will be updated dynamically

	footerStyle = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder(), true, false, false, false).
			BorderForeground(mutedColor).
			Padding(0, 1)

	// Message Bubbles
	ownBubbleStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FFFFFF")).
			Background(secondaryColor).
			Padding(0, 1).
			MarginTop(1).
			MarginLeft(4). // Indent from left
			Border(lipgloss.RoundedBorder()).
			BorderForeground(secondaryColor)

	otherBubbleStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#FFFFFF")).
				Background(otherColor).
				Padding(0, 1).
				MarginTop(1).
				MarginRight(4). // Indent from right
				Border(lipgloss.RoundedBorder()).
				BorderForeground(otherColor)

	// Legacy styles for compatibility
	ownMessageStyle = lipgloss.NewStyle().
			Foreground(secondaryColor)
	otherMessageStyle = lipgloss.NewStyle().
				Foreground(primaryColor)

	// Legacy selected style
	selectedStyle = lipgloss.NewStyle().
			Foreground(secondaryColor).
			Bold(true)

	timestampStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#D1D5DB")). // Light gray
			Faint(true)
)

const asciiArt = `
  ██████╗██╗     ██████╗ ███████╗███╗   ███╗███████╗ ██████╗ 
 ██╔════╝██║     ██╔══██╗╚══███╔╝████╗ ████║██╔════╝██╔════╝ 
 ██║     ██║     ██║  ██║  ███╔╝ ██╔████╔██║███████╗██║  ███╗
 ██║     ██║     ██║  ██║ ███╔╝  ██║╚██╔╝██║╚════██║██║   ██║
 ╚██████╗███████╗██████╔╝███████╗██║ ╚═╝ ██║███████║╚██████╔╝
  ╚═════╝╚══════╝╚═════╝ ╚══════╝╚═╝     ╚═╝╚══════╝ ╚═════╝ 
`

// --- View State ---

type pane int

const (
	paneSidebar pane = iota
	paneChat
	paneAuth // Special pane for full-screen auth
)

// Legacy view state (for compatibility if needed, though we should migrate)
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
	ID          int       `json:"id"`
	Name        *string   `json:"name"`
	IsGroup     bool      `json:"is_group"`
	CreatedAt   time.Time `json:"created_at"`
	UnreadCount int       `json:"unread_count"`
	LastMessage *Message  `json:"last_message,omitempty"` // For sidebar preview
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

type typingTimeoutMsg struct {
	userID int
}

// --- Main Model ---

type model struct {
	// Connection
	conn      *websocket.Conn
	serverURL string
	connected bool

	// Auth
	userID          int
	username        string
	authenticated   bool
	authAction      string // "login" or "register"
	serverInput     textinput.Model
	usernameInput   textinput.Model
	passwordInput   textinput.Model
	authFocused     int // 0=server, 1=username, 2=password
	authError       string
	isLoading       bool     // New: Track auth request state
	savedSession    *Session // For auto-login
	pendingPassword string   // Password to save after successful auth

	// Typing
	lastTypingSent time.Time
	typingUsers    map[int]string // userID -> username (if typing)

	// UI layout
	width       int
	height      int
	focusedPane pane // paneSidebar or paneChat

	// Sidebar
	conversations []Conversation
	selectedConv  int // Index in the list
	sidebarWidth  int

	// Chat
	currentConvID   int
	currentConvName string
	messages        []Message
	messageInput    textinput.Model
	chatViewport    viewport.Model

	// New Conversation Overlay
	showNewConv    bool
	newConvInput   textinput.Model
	newConvIsGroup bool
	newConvUsers   []string

	// Help
	showHelp bool

	// Info Overlay
	showInfo  bool
	infoInput textinput.Model
	infoMode  string // "rename" or "add_user"

	// System
	err            error
	reconnectCount int

	// Legacy view state
	view viewState
}

type wsReconnect struct{}

func initialModel(serverURL string) model {
	serverInput := textinput.New()
	serverInput.Placeholder = "wss://cldzmsg.cloudzz.dev/ws"
	if serverURL != "" {
		serverInput.SetValue(serverURL)
	} else {
		serverInput.SetValue("wss://cldzmsg.cloudzz.dev/ws")
	}
	serverInput.CharLimit = 128
	serverInput.Width = 40
	serverInput.Focus()

	usernameInput := textinput.New()
	usernameInput.Placeholder = "Username"
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

	// Load saved session for auto-login
	savedSession := loadSession()

	infoInput := textinput.New()
	infoInput.CharLimit = 32
	infoInput.Width = 30

	return model{
		serverURL:     serverURL,
		authAction:    "login",
		serverInput:   serverInput,
		usernameInput: usernameInput,
		passwordInput: passwordInput,
		messageInput:  messageInput,
		newConvInput:  newConvInput,
		infoInput:     infoInput,
		chatViewport:  chatViewport,
		focusedPane:   paneAuth, // Start at auth
		savedSession:  savedSession,
		sidebarWidth:  30,       // Fixed sidebar width
		view:          viewAuth, // Initialize legacy view state
		typingUsers:   make(map[int]string),
	}
}

// --- Commands ---

func connectToServer(url string) tea.Cmd {
	return func() tea.Msg {
		f, _ := os.OpenFile("debug.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		fmt.Fprintf(f, "Dialing WebSocket: %s\n", url)
		f.Close()

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
			f, _ := os.OpenFile("debug.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
			fmt.Fprintf(f, "FAILED to send message (%s): Connection is nil\n", msgType)
			f.Close()
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
	// Don't connect on startup - connection happens when user submits login form
	return textinput.Blink
}

// --- Update ---

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		// Global keys
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "?":
			if m.authenticated && m.showHelp {
				m.showHelp = false
				return m, nil
			}
			// Allow ? in inputs
			if m.focusedPane == paneChat || m.showNewConv || !m.authenticated {
				break
			}
			m.showHelp = !m.showHelp
			return m, nil
		case "ctrl+q", "esc":
			if m.showHelp {
				m.showHelp = false
				return m, nil
			}
			if m.showInfo {
				m.showInfo = false
				m.infoMode = ""
				return m, nil
			}
			if m.showNewConv {
				m.showNewConv = false
				return m, nil
			}
			// If in chat, focus sidebar
			if m.authenticated && m.focusedPane == paneChat {
				m.focusedPane = paneSidebar
				m.messageInput.Blur()
				return m, nil
			}
		case "q":
			// Only quit if in sidebar or auth, otherwise handled above/below
			if m.focusedPane == paneSidebar || !m.authenticated {
				return m, tea.Quit
			}
		}

		// Info Overlay Handling
		if m.showInfo {
			switch msg.String() {
			case "a":
				if m.infoMode == "" {
					m.infoMode = "add_user"
					m.infoInput.Placeholder = "Username to add..."
					m.infoInput.Focus()
					m.infoInput.SetValue("")
					return m, nil
				}
			case "r":
				if m.infoMode == "" {
					m.infoMode = "rename"
					m.infoInput.Placeholder = "New group name..."
					m.infoInput.Focus()
					m.infoInput.SetValue("")
					return m, nil
				}
			case "L":
				if m.infoMode == "" {
					conv := m.conversations[m.selectedConv]
					m.showInfo = false
					return m, m.sendWSMessage("leave_conversation", map[string]int{
						"conversation_id": conv.ID,
					})
				}
			case "enter":
				if m.infoMode != "" && m.infoInput.Value() != "" {
					conv := m.conversations[m.selectedConv]
					val := m.infoInput.Value()
					m.infoInput.SetValue("")
					mode := m.infoMode
					m.infoMode = ""
					m.showInfo = false

					if mode == "add_user" {
						return m, m.sendWSMessage("add_participant", map[string]interface{}{
							"conversation_id": conv.ID,
							"username":        val,
						})
					} else {
						return m, m.sendWSMessage("rename_conversation", map[string]interface{}{
							"conversation_id": conv.ID,
							"name":            val,
						})
					}
				}
			}
			m.infoInput, _ = m.infoInput.Update(msg)
			return m, nil
		}

		// Auth View Handling
		if !m.authenticated {
			// DEBUG: Log key press
			f, _ := os.OpenFile("debug.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
			fmt.Fprintf(f, "Key pressed: %q | Server: %q | User: %q | Pass: %q\n", msg.String(), m.serverInput.Value(), m.usernameInput.Value(), m.passwordInput.Value())
			f.Close()

			switch msg.String() {
			case "tab":
				// Cycle through server (0) -> username (1) -> password (2) -> server (0)
				m.serverInput.Blur()
				m.usernameInput.Blur()
				m.passwordInput.Blur()
				m.authFocused = (m.authFocused + 1) % 3
				switch m.authFocused {
				case 0:
					m.serverInput.Focus()
				case 1:
					m.usernameInput.Focus()
				case 2:
					m.passwordInput.Focus()
				}
			case "ctrl+r":
				if m.authAction == "login" {
					m.authAction = "register"
				} else {
					m.authAction = "login"
				}
			case "enter":
				f, _ := os.OpenFile("debug.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
				fmt.Fprintf(f, "Enter pressed. Values valid? %v\n", m.serverInput.Value() != "" && m.usernameInput.Value() != "" && m.passwordInput.Value() != "")
				f.Close()

				if m.serverInput.Value() != "" && m.usernameInput.Value() != "" && m.passwordInput.Value() != "" {
					m.isLoading = true // Set loading
					m.authError = ""   // Clear previous error
					m.pendingPassword = m.passwordInput.Value()
					m.serverURL = m.serverInput.Value()

					// Log debug
					f, _ := os.OpenFile("debug.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
					fmt.Fprintf(f, "Attempting auth: Server=%s Action=%s User=%s\n", m.serverURL, m.authAction, m.usernameInput.Value())
					f.Close()

					// Connect to server, then auth will happen in wsConnected handler
					return m, connectToServer(m.serverURL)
				}
			}
			// Update the focused input
			switch m.authFocused {
			case 0:
				m.serverInput, _ = m.serverInput.Update(msg)
			case 1:
				m.usernameInput, _ = m.usernameInput.Update(msg)
			case 2:
				m.passwordInput, _ = m.passwordInput.Update(msg)
			}
			return m, nil
		}

		// Authenticated View Handling
		if m.showNewConv {
			// New Conversation Modal
			switch msg.String() {
			case "enter":
				if m.newConvInput.Value() != "" {
					username := m.newConvInput.Value()
					m.newConvInput.SetValue("")
					m.newConvUsers = append(m.newConvUsers, username)
				}
			case "ctrl+g":
				m.newConvIsGroup = !m.newConvIsGroup
			case "ctrl+s":
				if len(m.newConvUsers) > 0 {
					var name string
					if m.newConvIsGroup {
						name = fmt.Sprintf("Group: %s", strings.Join(m.newConvUsers, ", "))
					}
					m.showNewConv = false
					// We stay on sidebar until conv is created and returned
					return m, m.sendWSMessage("create_conversation", map[string]interface{}{
						"name":      name,
						"is_group":  m.newConvIsGroup,
						"usernames": m.newConvUsers,
					})
				}
			}
			m.newConvInput, _ = m.newConvInput.Update(msg)
			return m, nil
		}

		switch m.focusedPane {
		case paneSidebar:
			switch msg.String() {
			case "up", "k":
				if m.selectedConv > 0 {
					m.selectedConv--
				}
			case "down", "j":
				if m.selectedConv < len(m.conversations)-1 {
					m.selectedConv++
				}
			case "enter", "l", "right":
				if len(m.conversations) > 0 {
					conv := m.conversations[m.selectedConv]
					// If switching conversation
					if conv.ID != m.currentConvID {
						m.currentConvID = conv.ID
						m.messages = nil // Clear previous messages
						m.updateChatViewport()

						if conv.Name != nil {
							m.currentConvName = *conv.Name
						} else {
							m.currentConvName = fmt.Sprintf("DM #%d", conv.ID)
						}

						cmds = append(cmds, m.sendWSMessage("get_messages", map[string]int{
							"conversation_id": conv.ID,
						}))
					}
					m.focusedPane = paneChat
					m.messageInput.Focus()
				}
			case "n":
				m.showNewConv = true
				m.newConvInput.Focus()
				m.newConvUsers = []string{}
			// Provide logout option
			case "L":
				clearSession()
				return m, tea.Quit // Or reset state to auth, but quit is safer for now
			}

		case paneChat:
			switch msg.String() {
			case "esc": // Back to sidebar navigation
				m.focusedPane = paneSidebar
				m.messageInput.Blur()
			case "i":
				m.showInfo = true
				m.infoMode = ""
			case "enter":
				if m.messageInput.Value() != "" {
					content := m.messageInput.Value()
					m.messageInput.SetValue("")
					cmds = append(cmds, m.sendWSMessage("send_message", map[string]interface{}{
						"conversation_id": m.currentConvID,
						"content":         content,
					}))
				}
			}
			m.messageInput, _ = m.messageInput.Update(msg)
			m.chatViewport, _ = m.chatViewport.Update(msg)
		}

	case typingTimeoutMsg:
		delete(m.typingUsers, msg.userID)

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

		// Recalculate layout
		m.sidebarWidth = m.width / 4
		if m.sidebarWidth < 25 {
			m.sidebarWidth = 25
		}

		sidebarStyle = sidebarStyle.Width(m.sidebarWidth - 2).Height(m.height - 2) // -2 for borders/padding

		chatWidth := m.width - m.sidebarWidth - 4 // -4 for margins/borders
		chatHeight := m.height - 2

		chatWindowStyle = chatWindowStyle.Width(chatWidth).Height(chatHeight)
		headerStyle = headerStyle.Width(chatWidth - 2)
		footerStyle = footerStyle.Width(chatWidth - 2)

		// Viewport takes remaining height: Total - Header - Footer - Borders
		viewportHeight := chatHeight - 4 - 3 // Approximate
		m.chatViewport = viewport.New(chatWidth-4, viewportHeight)
		m.messageInput.Width = chatWidth - 6

		m.updateChatViewport()

	case wsConnected:
		// DEBUG: Log connection success
		f, _ := os.OpenFile("debug.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		fmt.Fprintf(f, "WebSocket Connected successfully to %s\n", m.serverURL)
		f.Close()

		m.conn = msg.conn
		m.connected = true
		m.reconnectCount = 0 // Reset reconnect counter on successful connection

		// If we just submitted the login form (isLoading), send auth now
		if m.isLoading {
			return m, tea.Batch(
				listenForMessages(m.conn),
				m.sendWSMessage("auth", map[string]string{
					"username": m.usernameInput.Value(),
					"password": m.passwordInput.Value(),
					"action":   m.authAction,
				}),
			)
		}

		// Auto-login if we have a saved session
		if m.savedSession != nil {
			m.pendingPassword = m.savedSession.Password
			return m, tea.Batch(
				listenForMessages(m.conn),
				m.sendWSMessage("auth", map[string]string{
					"username": m.savedSession.Username,
					"password": m.savedSession.Password,
					"action":   "login",
				}),
			)
		}

		return m, listenForMessages(m.conn)

	case wsError:
		m.connected = false
		m.conn = nil

		// DEBUG: Log connection error
		f, _ := os.OpenFile("debug.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		fmt.Fprintf(f, "WebSocket Connection Error (Count: %d): %v\n", m.reconnectCount, msg.err)
		f.Close()

		if m.reconnectCount < 5 {
			m.reconnectCount++
			delay := time.Second * time.Duration(m.reconnectCount)
			return m, tea.Tick(delay, func(t time.Time) tea.Msg {
				return wsReconnect{}
			})
		}
		m.err = msg.err
		return m, nil

	case wsReconnect:
		return m, connectToServer(m.serverURL)

	case wsIncoming:
		// DEBUG: Log incoming message
		f, _ := os.OpenFile("debug.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		fmt.Fprintf(f, "Received WS Message: %s\n", string(msg.data))
		f.Close()

		var wsMsg struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(msg.data, &wsMsg); err != nil {
			f, _ := os.OpenFile("debug.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
			fmt.Fprintf(f, "JSON Error: %v\n", err)
			f.Close()
			return m, nil
		}

		f, _ = os.OpenFile("debug.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		fmt.Fprintf(f, "Processing Message Type: %s\n", wsMsg.Type)
		f.Close()

		switch wsMsg.Type {
		case "auth_success":
			m.isLoading = false
			var resp struct {
				UserID        int            `json:"user_id"`
				Username      string         `json:"username"`
				Conversations []Conversation `json:"conversations"`
			}
			json.Unmarshal(msg.data, &resp)
			m.userID = resp.UserID
			m.username = resp.Username
			m.conversations = resp.Conversations
			m.authenticated = true
			m.focusedPane = paneSidebar
			m.authError = ""

			// Save session for future auto-login
			if m.pendingPassword != "" {
				saveSession(resp.Username, m.pendingPassword)
				m.pendingPassword = ""
			}

		case "auth_error":
			m.isLoading = false
			var resp struct {
				Error string `json:"error"`
			}
			json.Unmarshal(msg.data, &resp)
			m.authError = resp.Error

			// Clear saved session if auto-login failed
			if m.savedSession != nil {
				clearSession()
				m.savedSession = nil
			}

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

			// Update conversations list (unread count and bump to top)
			foundIdx := -1
			for i, conv := range m.conversations {
				if conv.ID == resp.Message.ConversationID {
					foundIdx = i
					break
				}
			}

			if foundIdx != -1 {
				conv := m.conversations[foundIdx]
				if resp.Message.ConversationID != m.currentConvID {
					conv.UnreadCount++
				}
				conv.LastMessage = &resp.Message

				// Remove and prepend
				m.conversations = append(m.conversations[:foundIdx], m.conversations[foundIdx+1:]...)
				m.conversations = append([]Conversation{conv}, m.conversations...)

				// Adjust selected index
				if m.selectedConv == foundIdx {
					m.selectedConv = 0
				} else if foundIdx > m.selectedConv {
					// Conv below moved to top, our index shifts down
					m.selectedConv++
				}
			}

			if resp.Message.ConversationID == m.currentConvID {
				m.messages = append(m.messages, resp.Message)
				m.updateChatViewport()
				// Send read receipt if active
				cmds = append(cmds, m.sendWSMessage("read_receipt", map[string]int{
					"conversation_id": m.currentConvID,
				}))
				// Clear any typing indicator for this user if they just sent a message
				delete(m.typingUsers, resp.Message.SenderID)
			}

		case "typing":
			var resp struct {
				ConversationID int    `json:"conversation_id"`
				UserID         int    `json:"user_id"`
				Username       string `json:"username"`
			}
			json.Unmarshal(msg.data, &resp)
			if resp.ConversationID == m.currentConvID && resp.UserID != m.userID {
				m.typingUsers[resp.UserID] = resp.Username
				// Clear after 3 seconds
				cmds = append(cmds, tea.Tick(3*time.Second, func(t time.Time) tea.Msg {
					return typingTimeoutMsg{userID: resp.UserID}
				}))
			}
		}

		if m.conn != nil {
			cmds = append(cmds, listenForMessages(m.conn))
		}
	}

	// Typing indicator detection
	if m.authenticated && m.focusedPane == paneChat && m.messageInput.Value() != "" {
		if time.Since(m.lastTypingSent) > 2*time.Second {
			m.lastTypingSent = time.Now()
			cmds = append(cmds, m.sendWSMessage("typing", map[string]int{
				"conversation_id": m.currentConvID,
			}))
		}
	}

	// Update text inputs
	if !m.authenticated {
		if m.authFocused == 0 {
			m.usernameInput, _ = m.usernameInput.Update(msg)
		} else {
			m.passwordInput, _ = m.passwordInput.Update(msg)
		}
	} else if m.showNewConv {
		m.newConvInput, _ = m.newConvInput.Update(msg)
	} else if m.focusedPane == paneChat {
		m.messageInput, _ = m.messageInput.Update(msg)
		m.chatViewport, _ = m.chatViewport.Update(msg)
	}

	return m, tea.Batch(cmds...)
}

func (m *model) updateChatViewport() {
	m.chatViewport.SetContent(m.renderChatContent())
	m.chatViewport.GotoBottom()
}

func (m *model) renderChatContent() string {
	var content strings.Builder
	for _, msg := range m.messages {
		timestamp := msg.CreatedAt.Format("15:04")
		var style lipgloss.Style
		if msg.SenderID == m.userID {
			style = ownMessageStyle
		} else {
			style = otherMessageStyle
		}

		// Wrap text based on viewport width
		maxWidth := m.chatViewport.Width - 10 // Leave room for timestamp/username
		if maxWidth < 10 {
			maxWidth = 10
		}

		wrappedContent := fitString(msg.Content, maxWidth)

		line := fmt.Sprintf("%s %s: %s",
			mutedStyle.Render(timestamp),
			style.Render(msg.SenderUsername),
			wrappedContent,
		)
		content.WriteString(line + "\n")
	}
	return content.String()
}

// Simple word wrap helper
func fitString(s string, width int) string {
	if len(s) <= width {
		return s
	}
	// Just a basic cut for now to prevent explosion, lipgloss usually handles basic wrapping
	// But specific wrapping logic can be added here
	return s
}

// --- View ---

func (m model) View() string {
	if m.err != nil {
		return errorStyle.Render(fmt.Sprintf("Error: %v\n\nPress q to quit.", m.err))
	}

	if !m.authenticated {
		return m.authView()
	}

	// Main Layout: Sidebar + Chat Window
	mainView := lipgloss.JoinHorizontal(
		lipgloss.Top,
		m.sidebarView(),
		m.chatWindowView(),
	)

	if m.showHelp {
		return m.overlayHelp(mainView)
	}

	if m.showInfo {
		return m.overlayInfo(mainView)
	}

	return mainView
}

func (m model) overlayHelp(base string) string {
	width := 50
	height := 16

	var s strings.Builder
	s.WriteString(titleStyle.Render("Help & Controls") + "\n\n")

	// Two distinct columns
	s.WriteString(profileStyle.Render("Sidebar") + "\n")
	s.WriteString("  ↑/k, ↓/j  Navigate\n")
	s.WriteString("  Enter/l   Select Chat\n")
	s.WriteString("  n         New Chat\n")
	s.WriteString("  L         Logout\n\n")

	s.WriteString(profileStyle.Render("Chat") + "\n")
	s.WriteString("  Types     Type message\n")
	s.WriteString("  Enter     Send\n")
	s.WriteString("  Esc       Back to Sidebar\n\n")

	s.WriteString(profileStyle.Render("Global") + "\n")
	s.WriteString("  ?         Toggle Help\n")
	s.WriteString("  Ctrl+C    Quit\n")
	s.WriteString("  Tab       Switch Focus")

	modal := lipgloss.NewStyle().
		Width(width).Height(height).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(activeBorder).
		Background(bgColor).
		Padding(1, 2).
		Render(s.String())

	return lipgloss.Place(
		m.width, m.height,
		lipgloss.Center, lipgloss.Center,
		modal,
		lipgloss.WithWhitespaceChars(" "),
		lipgloss.WithWhitespaceForeground(lipgloss.NoColor{}),
	)
}

func (m model) overlayInfo(base string) string {
	width := 50
	height := 14

	var s strings.Builder
	s.WriteString(titleStyle.Render("Conversation Options") + "\n\n")

	if m.infoMode == "" {
		s.WriteString("  [r] Rename Group\n")
		s.WriteString("  [a] Add User\n")
		s.WriteString("  [L] Leave Conversation\n\n")
		s.WriteString(mutedStyle.Render("  Esc to cancel"))
	} else if m.infoMode == "rename" {
		s.WriteString("New Name:\n")
		s.WriteString(m.infoInput.View())
		s.WriteString("\n\n" + mutedStyle.Render("Enter to save, Esc to cancel"))
	} else if m.infoMode == "add_user" {
		s.WriteString("Add User:\n")
		s.WriteString(m.infoInput.View())
		s.WriteString("\n\n" + mutedStyle.Render("Enter to add, Esc to cancel"))
	}

	modal := lipgloss.NewStyle().
		Width(width).Height(height).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(activeBorder).
		Background(bgColor).
		Padding(1, 2).
		Render(s.String())

	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, modal)
}

func (m model) sidebarView() string {
	var s strings.Builder

	borderColor := mutedColor
	if m.focusedPane == paneSidebar {
		borderColor = activeBorder
	}

	style := sidebarStyle.Copy().BorderForeground(borderColor)

	s.WriteString(titleStyle.Render(fmt.Sprintf("%s (%s)", m.username, profileName)))
	s.WriteString("\n\n")

	if len(m.conversations) == 0 {
		s.WriteString(mutedStyle.Render("No conversations.\n'n' to create."))
	} else {
		for i, conv := range m.conversations {
			name := "DM"
			if conv.Name != nil {
				name = *conv.Name
			} else {
				name = fmt.Sprintf("Conv #%d", conv.ID)
			}
			// Icon
			icon := "👤"
			if conv.IsGroup {
				icon = "👥"
			}

			// Unread Badge
			unread := ""
			if conv.UnreadCount > 0 {
				unread = errorStyle.Render(fmt.Sprintf(" (%d)", conv.UnreadCount))
			}

			line := fmt.Sprintf("%s %s%s", icon, name, unread)

			if i == m.selectedConv {
				s.WriteString(selectedItemStyle.Render(line) + "\n")
			} else {
				s.WriteString(unselectedItemStyle.Render(line) + "\n")
			}
		}
	}

	// Helper text at bottom?
	return style.Render(s.String())
}

func (m model) chatWindowView() string {
	if m.showNewConv {
		// Overlay logic could be handled here, or just render over the chat
		return m.newConversationView()
	}

	if m.currentConvID == 0 {
		return chatWindowStyle.Render(
			lipgloss.Place(
				m.width-m.sidebarWidth-6,
				m.height-4,
				lipgloss.Center, lipgloss.Center,
				mutedStyle.Render("Select a conversation to start chatting"),
			),
		)
	}

	borderColor := mutedColor
	if m.focusedPane == paneChat {
		borderColor = activeBorder
	}

	// Header
	header := headerStyle.Render("💬 " + m.currentConvName)

	// Typing Status
	typingStatus := ""
	if len(m.typingUsers) > 0 {
		var names []string
		for _, name := range m.typingUsers {
			names = append(names, name)
		}
		typingStatus = mutedStyle.Render(fmt.Sprintf(" %s typing...", strings.Join(names, ", ")))
	}

	// Footer (Input)
	footerContent := m.messageInput.View()
	if typingStatus != "" {
		footerContent = typingStatus + "\n" + footerContent
	}
	footer := footerStyle.Render(footerContent)

	// Viewport rendered string
	vp := m.chatViewport.View()

	// Combine
	content := lipgloss.JoinVertical(
		lipgloss.Left,
		header,
		vp,
		footer,
	)

	return chatWindowStyle.Copy().BorderForeground(borderColor).Render(content)
}

func (m model) authView() string {
	// Reusing previous auth view logic but centered
	var s strings.Builder

	// Render ASCII Art
	banner := titleStyle.Foreground(primaryColor).Render(asciiArt)
	s.WriteString(banner + "\n\n")

	s.WriteString("Profile: " + profileStyle.Render(profileName) + "\n\n")

	action := m.authAction
	if action == "login" {
		s.WriteString("→ Login / Register\n\n")
	} else {
		s.WriteString("Login / → Register\n\n")
	}

	s.WriteString("Server:   " + m.serverInput.View() + "\n")
	s.WriteString("Username: " + m.usernameInput.View() + "\n")
	s.WriteString("Password: " + m.passwordInput.View() + "\n\n")

	if m.authError != "" {
		s.WriteString(errorStyle.Render(m.authError) + "\n")
	}

	if m.isLoading {
		s.WriteString(mutedStyle.Render("Connecting..."))
	} else {
		s.WriteString(mutedStyle.Render("Enter to Submit • Tab to Switch Field • Ctrl+R Toggle Mode"))
	}

	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, boxStyle.Render(s.String()))
}

func (m model) newConversationView() string {
	var s strings.Builder
	s.WriteString(titleStyle.Render("New Conversation") + "\n\n")

	if m.newConvIsGroup {
		s.WriteString("Type: Group (Ctrl+G to toggle)\n")
	} else {
		s.WriteString("Type: DM (Ctrl+G to toggle)\n")
	}

	s.WriteString("Add user: " + m.newConvInput.View() + "\n\n")

	if len(m.newConvUsers) > 0 {
		s.WriteString("Users to add:\n")
		for _, u := range m.newConvUsers {
			s.WriteString("- " + u + "\n")
		}
	}

	s.WriteString("\n(Ctrl+S to Create, Esc to Cancel)")

	// Render as a centered modal
	return lipgloss.Place(m.width-m.sidebarWidth-6, m.height-4, lipgloss.Center, lipgloss.Center, boxStyle.Render(s.String()))
}

// --- Main ---

func main() {
	// Parse flags
	flag.StringVar(&profileName, "profile", "default", "Profile name for session isolation")
	flag.Parse()

	serverURL := os.Getenv("CLDZMSG_SERVER")
	if serverURL == "" {
		serverURL = "ws://localhost:3567/ws"
	}

	p := tea.NewProgram(initialModel(serverURL), tea.WithAltScreen(), tea.WithMouseCellMotion())
	if _, err := p.Run(); err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}
}
