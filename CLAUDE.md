# AltMount Project

AltMount is a NNTP-based file mounting system with health monitoring and queue management capabilities.

## Project Structure

### Backend (Go)
- **Main**: Go application with REST API
- **Health System**: Monitors file integrity with automatic cleanup of healed files
- **Queue System**: Manages NZB processing and retry logic
- **Database**: SQLite with migrations for health and queue tracking

### Frontend (React + TypeScript)
- **Build System**: Vite with React
- **Styling**: Tailwind CSS 4 + DaisyUI
- **Package Manager**: **Use Bun instead of npm**
- **Linting**: Biome (already configured)

## Development Commands

### Backend
```bash
go build ./cmd/altmount
go run ./cmd/altmount serve
go test ./...
```

### Frontend  
```bash
cd frontend
bun install          # Install dependencies
bun run dev          # Development server
bun run build        # Production build
bun run lint         # Biome linting
```

## Key Features
- **Health Monitoring**: Files marked as corrupted/partial → checked → automatically removed from DB when healed
- **Queue Management**: NZB processing with retry logic and status tracking
- **Real-time UI**: React frontend with live updates and responsive design

## Technical Notes
- Frontend uses Tailwind CSS 4 modern syntax (`@import "tailwindcss"`)
- Health system maintains retry logic but auto-deletes healed files
- Biome handles linting and formatting (ESLint replaced)
- Use Bun as package manager for faster operations