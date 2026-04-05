# OpenLoadBalancer Admin UI - Features

## Component Library

### UI Components (shadcn/ui)
- **Button** - Primary, secondary, outline, ghost, link variants
- **Card** - Container with header, content, footer
- **Badge** - Status indicators with variants
- **Avatar** - User/profile images with fallback
- **Tabs** - Tabbed navigation
- **Table** - Data tables with sorting
- **Dialog** - Modal dialogs
- **Input** - Text inputs
- **Textarea** - Multi-line text inputs
- **Label** - Form labels
- **Select** - Dropdown selects
- **Switch** - Toggle switches
- **Checkbox** - Checkboxes
- **DropdownMenu** - Context menus
- **Tooltip** - Hover tooltips
- **Separator** - Visual dividers
- **Skeleton** - Loading placeholders
- **Alert** - Alert banners (info, success, warning, error)
- **ScrollArea** - Custom scrollbars
- **Progress** - Progress bars

### Layout Components
- **RootLayout** - Main app layout with sidebar and header
- **Sidebar** - Navigation sidebar with mobile support and tooltips
- **Header** - Top header with breadcrumbs, notifications, user menu
- **Breadcrumbs** - Auto-generated navigation path

### Form Components
- **useForm** - Form state management with validation
- **FormField** - Form field wrapper with error display
- **FormError** - Form-level error display
- **FormSuccess** - Form success message display

### Validation Rules
- `required` - Field is required
- `minLength` / `maxLength` - String length constraints
- `pattern` - Regex pattern matching
- `email` - Email format validation
- `url` - URL format validation
- `min` / `max` - Number range validation
- `custom` - Custom validation function

### Dialog Components
- **ConfirmDialog** - Confirmation dialog with variants
- **useConfirmDialog** - Hook for easy confirmation prompts

### Visualization Components
- **SimpleLineChart** - Area/line charts with gradients
- **SimpleBarChart** - Bar charts
- **SimplePieChart** - Donut charts
- **ChartEmptyState** - Empty state for charts
- **HealthIndicator** - Status dots with pulse animation
- **HealthStatusBadge** - Status badges (healthy, unhealthy, warning, etc.)
- **HealthScoreRing** - Circular progress indicator
- **UptimeBadge** - Formatted uptime display

### Data Components
- **SearchFilter** - Search with optional filter dropdowns
- **ColumnVisibility** - Toggle table column visibility
- **Pagination** - Full pagination with page size selector
- **SimplePagination** - Previous/Next buttons
- **DataExport** - Export data as JSON or CSV

### Notification Components
- **NotificationCenter** - Dropdown notification panel
- **useNotifications** - Hook for managing notifications

### Loading Components
- **PageLoading** - Full page skeleton
- **TableLoading** - Table skeleton loader
- **CardLoading** - Card skeleton loader

### Error Handling
- **ErrorBoundary** - Route-level error boundary
- **PageErrorBoundary** - Component-level error boundary
- **ErrorState** - Error display with retry

### Empty States
- **EmptyState** - Reusable empty state with icon, title, description, action

## Custom Hooks

### useForm
Form state management with built-in validation:

```typescript
const form = useForm(
  { email: '', password: '' },
  {
    email: { required: true, email: true },
    password: { required: true, minLength: 8 }
  }
)
```

### useConfirmDialog
Easy confirmation dialogs:

```typescript
const { confirm, dialog } = useConfirmDialog()

const handleDelete = async () => {
  await confirm(
    {
      title: 'Delete Item?',
      description: 'This action cannot be undone.',
      variant: 'destructive'
    },
    async () => {
      await deleteItem()
    }
  )
}
```

### useNotifications
Notification management:

```typescript
const { notifications, addNotification, markAsRead } = useNotifications()

addNotification({
  title: 'Success',
  message: 'Backend created',
  type: 'success'
})
```

### useDebounce
Debounce values:

```typescript
const debouncedSearch = useDebounce(searchQuery, 300)
```

### useLocalStorage
Persist state to localStorage:

```typescript
const [settings, setSettings] = useLocalStorage('settings', defaultSettings)
```

### useMediaQuery / useIsMobile
Responsive design hooks:

```typescript
const isMobile = useIsMobile()
const isDarkMode = useMediaQuery('(prefers-color-scheme: dark)')
```

## Pages

### Dashboard
- Stats cards with loading states
- Real-time metrics display
- Charts (traffic, response codes)
- Health alerts
- Auto-refresh every 5 seconds

### Backends
- CRUD operations
- Form validation
- Search filtering
- Confirmation dialogs for deletion
- Health status badges
- Data export (JSON/CSV)
- Health check triggers

### Pools
- Pool management
- Algorithm selection (10 algorithms)
- Backend assignment
- Health check configuration

### Routes
- Routing rules
- Path patterns
- Priority ordering
- Pool assignment

### Listeners
- Protocol support (HTTP/HTTPS/TCP/UDP)
- TLS configuration
- Route assignment
- Enable/disable toggle

### Middleware
- Module configuration
- Enable/disable switches
- Settings per module

### WAF
- Module toggles
- Rule management
- Mode selection (enforce/monitor/disabled)
- Statistics display

### Certificates
- Certificate list
- Upload custom certificates
- Let's Encrypt integration
- Auto-renewal settings
- Expiration tracking

### Logs
- System logs
- Access logs
- WAF logs
- Audit logs
- Search and filtering
- Line count selection

### Settings
- General settings
- Logging configuration
- Notifications
- Security settings
- Advanced settings
- Config reload

### Login
- Authentication form
- Password visibility toggle
- Remember me option
- Error handling

## Features

### Theme Support
- Light/Dark mode
- System preference detection
- localStorage persistence

### Responsive Design
- Mobile sidebar with overlay
- Responsive tables
- Adaptive layouts
- Touch-friendly controls

### Accessibility
- Keyboard navigation
- Focus management
- ARIA labels
- Screen reader support

### Real-time Updates
- TanStack Query with polling
- Optimistic updates
- Background refetching

### Error Handling
- Global error boundary
- API error interception
- Toast notifications
- Retry mechanisms

### Data Export
- JSON export
- CSV export
- All list pages

### Search & Filter
- Real-time search
- Column filtering
- Debounced input

## API Integration

The UI expects a REST API with the following endpoints:

```
GET    /api/v1/backends
POST   /api/v1/backends
DELETE /api/v1/backends/:id
POST   /api/v1/backends/:id/healthcheck

GET    /api/v1/pools
POST   /api/v1/pools
DELETE /api/v1/pools/:name

GET    /api/v1/routes
POST   /api/v1/routes
DELETE /api/v1/routes/:id

GET    /api/v1/listeners
POST   /api/v1/listeners
DELETE /api/v1/listeners/:name

GET    /api/v1/config
PUT    /api/v1/config
POST   /api/v1/config/reload

GET    /api/v1/metrics
GET    /api/v1/certificates
GET    /api/v1/logs/:type
```

## Development

```bash
npm install
npm run dev      # Development server
npm run build    # Production build
npm run lint     # ESLint
npm run typecheck # TypeScript check
```

## Build Output

Production build outputs to `dist/` directory which is embedded into the Go binary via `embed.go`.
