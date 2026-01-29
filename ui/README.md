# Semspec Web UI

Web interface for Semspec development agent built with SvelteKit 2 and Svelte 5.

## Features

- **Chat Interface**: Primary interaction point for the agentic system
- **Real-time Updates**: SSE-based activity stream
- **Dark Theme**: Consistent dark UI design
- **Mock Mode**: Development without backend dependencies

## Quick Start

```bash
# Install dependencies
npm install

# Start development server (with mocks)
npm run dev

# Type check
npm run check

# Build for production
npm run build

# Preview production build
npm run preview
```

## Development

### Environment Variables

Copy `.env.example` to `.env` and configure:

```bash
# Enable mock API for development
VITE_USE_MOCKS=true

# Optional: Backend API URL (defaults to same origin)
# VITE_API_URL=http://localhost:8080
```

### Mock Mode

When `VITE_USE_MOCKS=true`, the UI uses simulated data:
- Sample chat messages and responses
- Mock loop data with various states
- Simulated SSE activity events

### Project Structure

```
src/
├── lib/
│   ├── api/          # HTTP client and mock layer
│   ├── stores/       # Svelte 5 runes-based state
│   ├── components/   # UI components
│   │   ├── chat/     # Chat-specific components
│   │   └── shared/   # Reusable components
│   ├── utils/        # Utility functions
│   └── types.ts      # TypeScript definitions
├── routes/
│   └── +page.svelte  # Chat view (main route)
└── app.css           # Design system tokens
```

## Production Deployment

The static adapter outputs to `build/`. Serve from Go:

```go
mux.Handle("/", http.FileServer(http.Dir("./ui/build")))
```

## Backend API Contract

When mock mode is disabled, the UI expects these endpoints:

| Endpoint | Method | Purpose |
|----------|--------|---------|
| `/api/router/loops` | GET | List active loops |
| `/api/router/message` | POST | Send chat message |
| `/api/router/loops/:id/signal` | POST | Send loop signal |
| `/api/health` | GET | System health |
| `/stream/activity` | SSE | Real-time events |

## Tech Stack

- **Framework**: SvelteKit 2 with Svelte 5 (runes)
- **Styling**: Vanilla CSS with custom properties
- **Icons**: Lucide
- **Build**: Vite with static adapter
