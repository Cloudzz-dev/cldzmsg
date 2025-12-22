# cldzmsg

A terminal-based real-time messaging application built with Go.

![TUI Preview](https://via.placeholder.com/800x400?text=Terminal+Messaging+App)

## Features

- **Real-time messaging** via WebSockets
- **Direct messages** and **group chats**
- **Beautiful TUI** built with Bubbletea
- **PostgreSQL** for persistent storage
- **Easy installation** with a single command

## Quick Start

### Install Client

```bash
curl -sSL https://raw.githubusercontent.com/cloudzz-dev/cldzmsg/master/install.sh | bash
```

### Set Server

```bash
export CLDZMSG_SERVER=ws://your-server-ip:8080/ws
```

### Run

```bash
cldzmsg
```

## Server Setup (Raspberry Pi 5)

### Prerequisites

```bash
# Install PostgreSQL
sudo apt install postgresql postgresql-contrib

# Create database
sudo -u postgres createdb cldzmsg

# Apply schema
sudo -u postgres psql cldzmsg < internal/db/schema.sql
```

### Run Server

```bash
# Set database URL
export DATABASE_URL="postgres://postgres@localhost/cldzmsg?sslmode=disable"

# Build and run
go build -o server ./cmd/server
./server
```

### Systemd Service (Optional)

Create `/etc/systemd/system/cldzmsg.service`:

```ini
[Unit]
Description=CLDZMSG Server
After=network.target postgresql.service

[Service]
Type=simple
User=pi
Environment=DATABASE_URL=postgres://localhost/cldzmsg?sslmode=disable
ExecStart=/home/pi/cldzmsg/server
Restart=always

[Install]
WantedBy=multi-user.target
```

Enable:

```bash
sudo systemctl enable cldzmsg
sudo systemctl start cldzmsg
```

## Keybindings

### Auth Screen
| Key | Action |
|-----|--------|
| Tab | Switch fields |
| Ctrl+R | Toggle Login/Register |
| Enter | Submit |
| q | Quit |

### Conversations
| Key | Action |
|-----|--------|
| ↑/↓ or j/k | Navigate |
| Enter | Open conversation |
| n | New conversation |
| q | Quit |

### Chat
| Key | Action |
|-----|--------|
| Enter | Send message |
| Esc | Go back |

### New Conversation
| Key | Action |
|-----|--------|
| Enter | Add user |
| Ctrl+G | Toggle DM/Group |
| Ctrl+S | Create conversation |
| Esc | Cancel |

## Development

```bash
# Build server
go build -o server ./cmd/server

# Build client
go build -o cldzmsg ./cmd/client

# Run tests
go test ./...
```

## License

MIT
