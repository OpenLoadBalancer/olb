# OpenLoadBalancer Admin UI

A modern, feature-rich Admin UI for OpenLoadBalancer built with React 19, Tailwind CSS 4, and shadcn/ui.

## Features

### Core
- **React 19** with StrictMode and latest features
- **Tailwind CSS 4.0 Beta** with CSS-first configuration
- **TypeScript 5.7** for type safety
- **shadcn/ui** components with Radix UI primitives
- **Dark/Light theme** support with system preference detection
- **Responsive design** for all screen sizes

### Navigation & UX
- **Command Palette** (Ctrl+K) - Quick navigation and actions
- **Keyboard Shortcuts** (?) - Full keyboard support
- **Breadcrumbs** - Contextual navigation
- **Tour Guide** - Onboarding with spotlight effect
- **Notifications Center** - Real-time alerts and messages

### Data Management
- **Data Tables** with sorting, filtering, pagination
- **TanStack Query v5** - Server state management
- **Auto-save** - Form drafts with localStorage backup
- **Bulk operations** - Select, delete, export multiple items

### Visualizations
- **Charts** (Recharts): Line, Bar, Pie
- **Real-time metrics** - WebSocket updates
- **Skeleton loading** - Smooth loading states

## Getting Started

```bash
cd internal/webui
npm install
npm run dev
```

## Keyboard Shortcuts

| Shortcut | Action |
|----------|--------|
| Ctrl+K | Open command palette |
| ? | Show keyboard shortcuts |
| Ctrl+Shift+D | Toggle dark mode |
| G D | Go to Dashboard |
| G B | Go to Backends |
| Ctrl+R | Refresh data |
| Ctrl+N | Create new item |

## Project Structure

```
src/
├── components/    # UI components
├── pages/         # Route pages
├── hooks/         # Custom hooks
├── lib/           # Utilities
├── providers/     # Context providers
└── types/         # TypeScript types
```
