#!/bin/bash
set -e

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
NC='\033[0m' # No Color

# Configuration
PORT="${PORT:-3567}"
COMPOSE_FILE="docker-compose.yml"

echo -e "${BLUE}"
echo "  ╔═══════════════════════════════════════════╗"
echo "  ║       CLDZMSG Server Deploy Script        ║"
echo "  ╚═══════════════════════════════════════════╝"
echo -e "${NC}"

# =============================================================================
# Dependency Checks
# =============================================================================

check_dependencies() {
    local missing=()
    
    echo -e "${CYAN}Checking dependencies...${NC}"
    
    # Check Docker
    if ! command -v docker &> /dev/null; then
        missing+=("docker")
        echo -e "  ${RED}✗${NC} Docker not found"
    else
        echo -e "  ${GREEN}✓${NC} Docker $(docker --version | cut -d' ' -f3 | tr -d ',')"
    fi
    
    # Check Docker Compose (v2 or standalone)
    if docker compose version &> /dev/null; then
        COMPOSE_CMD="docker compose"
        echo -e "  ${GREEN}✓${NC} Docker Compose $(docker compose version --short)"
    elif command -v docker-compose &> /dev/null; then
        COMPOSE_CMD="docker-compose"
        echo -e "  ${GREEN}✓${NC} docker-compose $(docker-compose --version | cut -d' ' -f4 | tr -d ',')"
    else
        missing+=("docker-compose")
        echo -e "  ${RED}✗${NC} Docker Compose not found"
    fi
    
    # Check Git (for commit/push)
    if ! command -v git &> /dev/null; then
        missing+=("git")
        echo -e "  ${RED}✗${NC} Git not found"
    else
        echo -e "  ${GREEN}✓${NC} Git $(git --version | cut -d' ' -f3)"
    fi
    
    if [ ${#missing[@]} -gt 0 ]; then
        echo ""
        echo -e "${YELLOW}Missing dependencies: ${missing[*]}${NC}"
        install_dependencies "${missing[@]}"
    fi
    
    echo ""
}

# =============================================================================
# Install Missing Dependencies
# =============================================================================

install_dependencies() {
    local deps=("$@")
    
    # Detect package manager
    if command -v apt-get &> /dev/null; then
        PKG_MANAGER="apt"
    elif command -v pacman &> /dev/null; then
        PKG_MANAGER="pacman"
    elif command -v dnf &> /dev/null; then
        PKG_MANAGER="dnf"
    elif command -v yum &> /dev/null; then
        PKG_MANAGER="yum"
    else
        echo -e "${RED}Could not detect package manager. Please install manually: ${deps[*]}${NC}"
        exit 1
    fi
    
    echo -e "${YELLOW}Would you like to install missing dependencies? [y/N]${NC}"
    read -r response
    if [[ ! "$response" =~ ^[Yy]$ ]]; then
        echo -e "${RED}Cannot proceed without dependencies.${NC}"
        exit 1
    fi
    
    for dep in "${deps[@]}"; do
        echo -e "${BLUE}Installing $dep...${NC}"
        case $dep in
            docker)
                case $PKG_MANAGER in
                    apt)
                        sudo apt-get update
                        sudo apt-get install -y docker.io
                        sudo systemctl enable docker
                        sudo systemctl start docker
                        sudo usermod -aG docker "$USER"
                        ;;
                    pacman)
                        sudo pacman -S --noconfirm docker
                        sudo systemctl enable docker
                        sudo systemctl start docker
                        sudo usermod -aG docker "$USER"
                        ;;
                    dnf|yum)
                        sudo $PKG_MANAGER install -y docker
                        sudo systemctl enable docker
                        sudo systemctl start docker
                        sudo usermod -aG docker "$USER"
                        ;;
                esac
                ;;
            docker-compose)
                case $PKG_MANAGER in
                    apt)
                        sudo apt-get install -y docker-compose-plugin || sudo apt-get install -y docker-compose
                        ;;
                    pacman)
                        sudo pacman -S --noconfirm docker-compose
                        ;;
                    dnf|yum)
                        sudo $PKG_MANAGER install -y docker-compose
                        ;;
                esac
                # Re-check compose command
                if docker compose version &> /dev/null; then
                    COMPOSE_CMD="docker compose"
                else
                    COMPOSE_CMD="docker-compose"
                fi
                ;;
            git)
                case $PKG_MANAGER in
                    apt) sudo apt-get install -y git ;;
                    pacman) sudo pacman -S --noconfirm git ;;
                    dnf|yum) sudo $PKG_MANAGER install -y git ;;
                esac
                ;;
        esac
        echo -e "  ${GREEN}✓${NC} $dep installed"
    done
    
    # Note about docker group
    if [[ " ${deps[*]} " =~ " docker " ]]; then
        echo -e "${YELLOW}Note: You may need to log out and back in for docker group membership to take effect.${NC}"
    fi
}

# =============================================================================
# Build and Deploy
# =============================================================================

deploy() {
    echo -e "${CYAN}Building and deploying...${NC}"
    
    # Check if compose file exists
    if [ ! -f "$COMPOSE_FILE" ]; then
        echo -e "${RED}Error: $COMPOSE_FILE not found${NC}"
        exit 1
    fi
    
    # Stop existing containers (if any)
    echo -e "${BLUE}Stopping existing containers...${NC}"
    $COMPOSE_CMD down 2>/dev/null || true
    
    # Build and start
    echo -e "${BLUE}Building images...${NC}"
    if ! $COMPOSE_CMD build; then
        echo -e "${RED}Error: Build failed${NC}"
        exit 1
    fi
    
    echo -e "${BLUE}Starting services...${NC}"
    if ! $COMPOSE_CMD up -d; then
        echo -e "${RED}Error: Failed to start services${NC}"
        exit 1
    fi
    
    echo ""
}

# =============================================================================
# Health Check
# =============================================================================

health_check() {
    echo -e "${CYAN}Running health checks...${NC}"
    
    # Wait for containers to be ready
    local max_attempts=30
    local attempt=1
    
    echo -e "${BLUE}Waiting for database to be healthy...${NC}"
    while [ $attempt -le $max_attempts ]; do
        if docker inspect cldzmsg-db --format='{{.State.Health.Status}}' 2>/dev/null | grep -q "healthy"; then
            echo -e "  ${GREEN}✓${NC} Database is healthy"
            break
        fi
        echo -e "  Attempt $attempt/$max_attempts..."
        sleep 2
        ((attempt++))
    done
    
    if [ $attempt -gt $max_attempts ]; then
        echo -e "  ${RED}✗${NC} Database health check timed out"
        echo -e "${RED}Checking container logs:${NC}"
        $COMPOSE_CMD logs --tail=20 postgres
        exit 1
    fi
    
    # Check server container
    if docker ps --format '{{.Names}}' | grep -q "cldzmsg-server"; then
        echo -e "  ${GREEN}✓${NC} Server container is running"
    else
        echo -e "  ${RED}✗${NC} Server container is not running"
        echo -e "${RED}Checking container logs:${NC}"
        $COMPOSE_CMD logs --tail=20 server
        exit 1
    fi
    
    # Test health endpoint
    sleep 2
    if curl -sf "http://localhost:$PORT/health" > /dev/null 2>&1; then
        echo -e "  ${GREEN}✓${NC} Health endpoint responding"
    else
        echo -e "  ${YELLOW}!${NC} Health endpoint not responding yet (may still be starting)"
    fi
    
    echo ""
}

# =============================================================================
# Commit and Push
# =============================================================================

commit_and_push() {
    echo -e "${CYAN}Checking for changes to commit...${NC}"
    
    # Check if we're in a git repo
    if ! git rev-parse --git-dir > /dev/null 2>&1; then
        echo -e "  ${YELLOW}!${NC} Not a git repository, skipping commit"
        return
    fi
    
    # Check for uncommitted changes
    if git diff --quiet && git diff --cached --quiet; then
        echo -e "  ${GREEN}✓${NC} No changes to commit"
        return
    fi
    
    echo -e "${BLUE}Staging changes...${NC}"
    git add -A
    
    echo -e "${BLUE}Committing changes...${NC}"
    local msg="chore: update deployment ($(date +%Y-%m-%d))"
    git commit -m "$msg"
    
    echo -e "${BLUE}Pushing to remote...${NC}"
    if git push; then
        echo -e "  ${GREEN}✓${NC} Changes pushed successfully"
    else
        echo -e "  ${YELLOW}!${NC} Push failed - you may need to set up remote or authenticate"
    fi
    
    echo ""
}

# =============================================================================
# Summary
# =============================================================================

print_summary() {
    echo -e "${GREEN}"
    echo "  ╔═══════════════════════════════════════════╗"
    echo "  ║           Deployment Complete!            ║"
    echo "  ╚═══════════════════════════════════════════╝"
    echo -e "${NC}"
    echo -e "  ${GREEN}✓${NC} Server running on port ${CYAN}$PORT${NC}"
    echo -e "  ${GREEN}✓${NC} WebSocket endpoint: ${CYAN}ws://localhost:$PORT/ws${NC}"
    echo -e "  ${GREEN}✓${NC} Health check: ${CYAN}http://localhost:$PORT/health${NC}"
    echo ""
    echo -e "  ${BLUE}To connect with the client:${NC}"
    echo -e "    ./cldzmsg"
    echo -e "    (Then enter your server URL on the login screen)"
    echo ""
    echo -e "  ${BLUE}To view logs:${NC}"
    echo -e "    $COMPOSE_CMD logs -f"
    echo ""
    echo -e "  ${BLUE}To stop:${NC}"
    echo -e "    $COMPOSE_CMD down"
    echo ""
}

# =============================================================================
# Main
# =============================================================================

main() {
    check_dependencies
    deploy
    health_check
    commit_and_push
    print_summary
}

main "$@"
