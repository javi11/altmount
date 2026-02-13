# AltMount Frontend Development Standards

Comprehensive coding standards and best practices for the AltMount React + TypeScript frontend.

## React Best Practices

### Component Structure

```tsx
// ✅ Good: Functional component with TypeScript
interface ComponentProps {
  title: string;
  isActive?: boolean;
  onAction: (id: string) => void;
}

export function ComponentName({ title, isActive = false, onAction }: ComponentProps) {
  const [state, setState] = useState<string>("");
  
  return (
    <div className="component-container">
      {/* Component content */}
    </div>
  );
}

// ❌ Avoid: Default exports, arrow functions for components
export default ({ title }) => { /* ... */ };
```

### TypeScript Guidelines

- **Always define interfaces** for component props and complex objects
- **Use strict typing** - avoid `any` type
- **Define return types** for complex functions
- **Use union types** for state enums and options
- **Run type checking**: Use `bun run check` for comprehensive TypeScript validation

```tsx
// ✅ Good: Strict typing
interface UserStatus {
  status: 'online' | 'offline' | 'away';
  lastSeen?: Date;
}

// ❌ Avoid: Loose typing
interface UserStatus {
  status: string;
  lastSeen: any;
}
```

### State Management

- **Use `useState` for local component state**
- **Use custom hooks** for shared state logic
- **Keep state minimal** - derive computed values
- **Batch state updates** when possible

```tsx
// ✅ Good: Custom hook for API state
function useConfig() {
  const [data, setData] = useState<ConfigResponse | null>(null);
  const [isLoading, setIsLoading] = useState(false);
  const [error, setError] = useState<Error | null>(null);
  
  // Hook logic here
  return { data, isLoading, error, refetch };
}

// ✅ Good: Using the hook
function ConfigPage() {
  const { data: config, isLoading, error, refetch } = useConfig();
  // Component logic
}
```

### Hook Best Practices

- **Custom hooks start with `use`**
- **Return objects not arrays** for multiple values
- **Use `useCallback` for event handlers** passed to children
- **Use `useMemo` for expensive calculations**

```tsx
// ✅ Good: Custom hook with clear return
function useApi<T>(endpoint: string) {
  const [data, setData] = useState<T | null>(null);
  const [loading, setLoading] = useState(false);
  
  const fetchData = useCallback(async () => {
    setLoading(true);
    try {
      const response = await fetch(endpoint);
      const result = await response.json();
      setData(result);
    } finally {
      setLoading(false);
    }
  }, [endpoint]);
  
  return { data, loading, fetchData };
}
```

## DaisyUI Component Guidelines

### Prefer DaisyUI Components

Always use DaisyUI components over custom CSS when available:

```tsx
// ✅ Good: Use DaisyUI components
<button type="button" className="btn btn-primary">
  Primary Action
</button>

<div className="card bg-base-100 shadow-lg">
  <div className="card-body">
    <h2 className="card-title">Card Title</h2>
    <p>Card content here</p>
  </div>
</div>

// ❌ Avoid: Custom styling when DaisyUI exists
<button 
  type="button" 
  className="px-4 py-2 bg-blue-500 text-white rounded hover:bg-blue-600"
>
  Custom Button
</button>
```

### DaisyUI Component Patterns

#### Buttons

```tsx
// Basic buttons
<button type="button" className="btn">Default</button>
<button type="button" className="btn btn-primary">Primary</button>
<button type="button" className="btn btn-secondary">Secondary</button>
<button type="button" className="btn btn-outline">Outline</button>

// Button sizes
<button type="button" className="btn btn-xs">Extra Small</button>
<button type="button" className="btn btn-sm">Small</button>
<button type="button" className="btn btn-lg">Large</button>

// Button states
<button type="button" className="btn btn-primary" disabled>Disabled</button>
<button type="button" className="btn btn-primary loading">Loading</button>
```

#### Cards

```tsx
<div className="card bg-base-100 shadow-lg">
  <div className="card-body">
    <h2 className="card-title">
      Card Title
      <div className="badge badge-secondary">NEW</div>
    </h2>
    <p>Card description text here</p>
    <div className="card-actions justify-end">
      <button type="button" className="btn btn-primary">Action</button>
    </div>
  </div>
</div>
```

#### Menus

```tsx
<ul className="menu bg-base-200 rounded-box">
  <li>
    <a className={activeItem === 'home' ? 'active' : ''}>
      <HomeIcon className="h-5 w-5" />
      Home
    </a>
  </li>
  <li>
    <a>
      <SettingsIcon className="h-5 w-5" />
      Settings
      <span className="badge badge-warning badge-xs">New</span>
    </a>
  </li>
</ul>
```

#### Forms

```tsx
// ✅ Good: Use DaisyUI fieldset for form inputs
<fieldset className="fieldset">
  <legend className="fieldset-legend">Input Label</legend>
  <input 
    type="text" 
    className="input" 
    placeholder="Enter text here"
  />
  <p className="label">Helper text</p>
</fieldset>

// ✅ Good: Fieldset with select dropdown
<fieldset className="fieldset">
  <legend className="fieldset-legend">Select Option</legend>
  <select className="select">
    <option value="">Choose an option</option>
    <option value="option1">Option 1</option>
    <option value="option2">Option 2</option>
  </select>
</fieldset>

// ✅ Good: Fieldset with checkbox
<fieldset className="fieldset">
  <legend className="fieldset-legend">Preferences</legend>
  <label className="cursor-pointer label">
    <span className="label-text">Remember me</span>
    <input type="checkbox" className="checkbox" />
  </label>
</fieldset>

// ❌ Avoid: Old form-control pattern
<div className="form-control">
  <label htmlFor="input-id" className="label">
    <span className="label-text">Input Label</span>
  </label>
  <input type="text" className="input input-bordered" />
</div>
```

#### Alerts

```tsx
<div className="alert alert-success">
  <CheckIcon className="h-6 w-6" />
  <div>Success message here</div>
</div>

<div className="alert alert-error">
  <XIcon className="h-6 w-6" />
  <div>
    <div className="font-bold">Error Title</div>
    <div className="text-sm">Error description</div>
  </div>
</div>
```

### Responsive Design with DaisyUI

```tsx
// Use DaisyUI responsive classes
<div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
  <div className="card">Card 1</div>
  <div className="card">Card 2</div>
  <div className="card">Card 3</div>
</div>

// DaisyUI responsive utilities
<div className="navbar">
  <div className="navbar-start">
    <div className="dropdown lg:hidden">
      {/* Mobile menu */}
    </div>
  </div>
  <div className="navbar-center hidden lg:flex">
    {/* Desktop menu */}
  </div>
</div>
```

## HTML Standards & Accessibility

### Button Standards

```tsx
// ✅ Always specify button type
<button type="button" onClick={handleClick}>Click Me</button>
<button type="submit" form="form-id">Submit</button>
<button type="reset" form="form-id">Reset</button>

// ✅ Use semantic button elements for actions
<button type="button" className="btn" onClick={handleEdit}>
  <EditIcon className="h-4 w-4" />
  Edit
</button>

// ❌ Avoid: Missing type attribute
<button onClick={handleClick}>Click Me</button>

// ❌ Avoid: Non-button elements for actions
<div onClick={handleClick} className="btn">Click Me</div>
```

### Form Standards

```tsx
// ✅ Good: Proper form structure with fieldset
<form onSubmit={handleSubmit}>
  <fieldset className="fieldset">
    <legend className="fieldset-legend">Email Address</legend>
    <input
      type="email"
      name="email"
      className="input"
      required
      aria-describedby="email-help"
    />
    <p id="email-help" className="label">
      We'll never share your email
    </p>
  </fieldset>
  
  <button type="submit" className="btn btn-primary">
    Submit
  </button>
</form>

// ✅ Good: Multi-field form with fieldsets
<form onSubmit={handleSubmit}>
  <div className="space-y-4">
    <fieldset className="fieldset">
      <legend className="fieldset-legend">Username</legend>
      <input
        type="text"
        name="username"
        className="input"
        required
      />
    </fieldset>
    
    <fieldset className="fieldset">
      <legend className="fieldset-legend">Password</legend>
      <input
        type="password"
        name="password"
        className="input"
        required
      />
    </fieldset>
  </div>
  
  <button type="submit" className="btn btn-primary">
    Submit
  </button>
</form>
```

### Accessibility Guidelines

```tsx
// ✅ Good: Accessible navigation
<nav aria-label="Main navigation">
  <ul className="menu menu-horizontal">
    <li><a href="/dashboard" aria-current="page">Dashboard</a></li>
    <li><a href="/files">Files</a></li>
    <li><a href="/settings">Settings</a></li>
  </ul>
</nav>

// ✅ Good: Screen reader support
<button
  type="button"
  className="btn btn-ghost"
  aria-label="Close dialog"
  onClick={handleClose}
>
  <XIcon className="h-4 w-4" aria-hidden="true" />
</button>

// ✅ Good: Loading states
{isLoading ? (
  <div className="loading loading-spinner" aria-label="Loading content" />
) : (
  <div>{content}</div>
)}
```

### Semantic HTML

```tsx
// ✅ Good: Semantic structure
<main>
  <header>
    <h1>Page Title</h1>
    <nav aria-label="Breadcrumb">
      {/* Breadcrumb navigation */}
    </nav>
  </header>
  
  <section aria-labelledby="section-title">
    <h2 id="section-title">Section Title</h2>
    <article>
      {/* Article content */}
    </article>
  </section>
  
  <aside aria-label="Related information">
    {/* Sidebar content */}
  </aside>
</main>
```

## Code Quality Standards

### Naming Conventions

```tsx
// ✅ Good: Clear, descriptive names
interface UserProfile {
  firstName: string;
  lastName: string;
  isActive: boolean;
}

function useUserAuthentication() { /* ... */ }
function validateEmailAddress(email: string): boolean { /* ... */ }

// ❌ Avoid: Unclear abbreviations
interface UsrProf {
  fName: string;
  lName: string;
  act: boolean;
}
```

### File Organization

```text
src/
├── components/
│   ├── ui/              # Reusable UI components
│   │   ├── Button.tsx
│   │   ├── Modal.tsx
│   │   └── index.ts
│   ├── auth/            # Domain-specific components
│   │   ├── LoginForm.tsx
│   │   └── index.ts
│   └── layout/          # Layout components
│       ├── Navbar.tsx
│       └── Sidebar.tsx
├── hooks/               # Custom hooks
│   ├── useAuth.ts
│   ├── useApi.ts
│   └── index.ts
├── pages/               # Page components
│   ├── Dashboard.tsx
│   └── ConfigurationPage.tsx
├── services/            # API and external services
│   ├── api.ts
│   └── webdavClient.ts
├── types/               # TypeScript type definitions
│   ├── auth.ts
│   ├── config.ts
│   └── index.ts
└── utils/               # Utility functions
    ├── format.ts
    └── validation.ts
```

### Import/Export Patterns

```tsx
// ✅ Good: Named exports
export function Button({ children, ...props }: ButtonProps) {
  return <button type="button" {...props}>{children}</button>;
}

export interface ButtonProps {
  children: React.ReactNode;
  variant?: 'primary' | 'secondary';
}

// ✅ Good: Direct imports (preferred)
import { Button } from '../ui/Button';
import { Modal } from '../ui/Modal';
import { useAuth } from '../../hooks/useAuth';
import type { UserProfile } from '../../types/auth';

// ✅ Good: Organized imports
import { useState, useEffect, useCallback } from 'react';
import { User, Settings } from 'lucide-react';

import { Button } from '../ui/Button';
import { Modal } from '../ui/Modal';
import { useAuth } from '../../hooks/useAuth';
import type { UserProfile } from '../../types/auth';

// ❌ Avoid: Barrel exports and index.ts files
// components/ui/index.ts (DON'T CREATE THIS)
export { Button } from './Button';
export { Modal } from './Modal';
export type { ButtonProps, ModalProps } from './types';

// ❌ Avoid: Importing from index files
import { Button, Modal } from '../ui';
```

**Why we forbid barrel exports:**

- **Build Performance**: Barrel exports can cause unnecessary bundling and slower builds
- **Tree Shaking Issues**: Can prevent proper dead code elimination
- **Circular Dependencies**: More prone to circular dependency issues
- **IDE Performance**: Can slow down TypeScript language server
- **Debugging**: Makes it harder to trace import paths
- **Explicit Dependencies**: Direct imports make dependencies more obvious

### Error Handling

```tsx
// ✅ Good: Comprehensive error handling
function useApi<T>(url: string) {
  const [data, setData] = useState<T | null>(null);
  const [error, setError] = useState<Error | null>(null);
  const [isLoading, setIsLoading] = useState(false);

  const fetchData = useCallback(async () => {
    try {
      setIsLoading(true);
      setError(null);

      const response = await fetch(url);
      if (!response.ok) {
        throw new Error(`HTTP error! status: ${response.status}`);
      }

      const result = await response.json();
      setData(result);
    } catch (err) {
      setError(err instanceof Error ? err : new Error('Unknown error'));
    } finally {
      setIsLoading(false);
    }
  }, [url]);

  return { data, error, isLoading, fetchData };
}

// ✅ Good: Error boundaries for components
function ErrorBoundary({ children }: { children: React.ReactNode }) {
  return (
    <ErrorBoundaryComponent
      fallback={({ error }) => (
        <div className="alert alert-error">
          <XCircleIcon className="h-6 w-6" />
          <div>
            <div className="font-bold">Something went wrong</div>
            <div className="text-sm">{error.message}</div>
          </div>
        </div>
      )}
    >
      {children}
    </ErrorBoundaryComponent>
  );
}
```

### Logging and Debugging

**Philosophy**: Log only what's necessary for debugging critical issues. Avoid excessive debug logging that clutters the console and impacts performance.

```tsx
// ✅ Good: Log critical errors and important state changes
function processImportQueue(items: ImportItem[]) {
  try {
    const results = items.map(processItem);
    console.info(`Processed ${results.length} import items`);
    return results;
  } catch (err) {
    console.error('Failed to process import queue:', err);
    throw err;
  }
}

// ✅ Good: Conditional debug logging
const DEBUG = import.meta.env.DEV;

function fetchData(url: string) {
  if (DEBUG) {
    console.debug('Fetching:', url);
  }
  // ... fetch logic
}

// ❌ Avoid: Excessive debug logs everywhere
function updateState(newValue: string) {
  console.log('updateState called');
  console.log('newValue:', newValue);
  console.log('previous state:', state);
  setState(newValue);
  console.log('state updated');
  console.log('new state:', newValue);
}

// ❌ Avoid: Logging every render or effect
useEffect(() => {
  console.log('Component rendered');
  console.log('Current props:', props);
  console.log('Current state:', state);
}, [props, state]);
```

**Logging Guidelines**:

- **Critical Errors**: Always log with `console.error()`
- **Important Events**: Use `console.info()` for significant operations
- **Development Only**: Use conditional `console.debug()` for detailed debugging
- **Production**: Avoid verbose logging in production builds
- **Performance**: Don't log in hot paths (loops, renders, frequent callbacks)
- **Privacy**: Never log sensitive data (passwords, tokens, PII)

## Icons and Assets

### Lucide React Icons

Use Lucide React for all icons throughout the application:

```tsx
// ✅ Good: Use Lucide React icons
import { Home, Settings, User, ChevronDown, X } from 'lucide-react';

function NavigationMenu() {
  return (
    <ul className="menu bg-base-200 rounded-box">
      <li>
        <a>
          <Home className="h-5 w-5" />
          Home
        </a>
      </li>
      <li>
        <a>
          <Settings className="h-5 w-5" />
          Settings
        </a>
      </li>
    </ul>
  );
}

// ✅ Good: Icon sizing with consistent classes
<button type="button" className="btn btn-ghost">
  <X className="h-4 w-4" aria-hidden="true" />
</button>

// ✅ Good: Icons in alerts
<div className="alert alert-success">
  <CheckCircle className="h-6 w-6" />
  <div>Success message here</div>
</div>
```

**Icon Guidelines:**

- **Consistent sizing**: Use `h-4 w-4` for small icons, `h-5 w-5` for medium, `h-6 w-6` for larger
- **Accessibility**: Add `aria-hidden="true"` for decorative icons
- **Semantic naming**: Import icons with descriptive names that match their usage
- **Performance**: Only import the specific icons you need

## Project-Specific Conventions

### API Integration Patterns

```tsx
// ✅ Use the established API client pattern
import { apiClient } from '../services/api';

function useConfig() {
  return useQuery({
    queryKey: ['config'],
    queryFn: () => apiClient.get<ConfigResponse>('/api/config'),
  });
}

function useUpdateConfig() {
  const queryClient = useQueryClient();
  
  return useMutation({
    mutationFn: (data: ConfigUpdateRequest) => 
      apiClient.patch('/api/config', data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['config'] });
    },
  });
}
```

### Component Structure for AltMount

```tsx
// ✅ Follow established patterns
interface PageProps {
  // Page-specific props
}

export function PageName() {
  // 1. Hooks and state
  const { data, isLoading, error } = useApiHook();
  const [localState, setLocalState] = useState();
  
  // 2. Event handlers
  const handleAction = useCallback(() => {
    // Handler logic
  }, [dependencies]);
  
  // 3. Early returns for loading/error states
  if (isLoading) {
    return (
      <div className="flex justify-center items-center min-h-[400px]">
        <div className="loading loading-spinner loading-lg" />
      </div>
    );
  }
  
  if (error) {
    return (
      <div className="alert alert-error">
        <XIcon className="h-6 w-6" />
        <div>{error.message}</div>
      </div>
    );
  }
  
  // 4. Main render
  return (
    <div className="space-y-6">
      {/* Page content */}
    </div>
  );
}
```

### Performance Guidelines

```tsx
// ✅ Good: Memoize expensive calculations
const expensiveValue = useMemo(() => {
  return data?.items.reduce((acc, item) => acc + item.value, 0);
}, [data?.items]);

// ✅ Good: Memoize callback functions
const handleItemClick = useCallback((id: string) => {
  onItemSelect(id);
  // Other logic
}, [onItemSelect]);

// ✅ Good: Lazy load heavy components
const HeavyComponent = lazy(() => import('./HeavyComponent'));

function App() {
  return (
    <Suspense fallback={<div className="loading loading-spinner" />}>
      <HeavyComponent />
    </Suspense>
  );
}
```

## Development Workflow

### Before Committing

1. **Run full build**: `make` (Build both backend and frontend, run all checks)
2. **Run type checking**: `bun run check` (TypeScript validation, linting, formatting, and code quality)
3. **Test build**: `bun run build` (TypeScript compilation + Vite build)
4. **Review changes**: Ensure code follows these standards

**Command Reference:**

- `make` - Full project build (Go backend + frontend, all validations) - **REQUIRED before commit**
- `bun run check` - Comprehensive validation (TypeScript + linting + formatting)
- `bun run lint` - Linting-only checks when needed
- `bun run build` - Production build validation

### Code Review Checklist

- [ ] Components use TypeScript interfaces
- [ ] DaisyUI components used where appropriate
- [ ] Buttons have `type` attribute
- [ ] Accessibility attributes present
- [ ] Error states handled
- [ ] Loading states implemented
- [ ] Responsive design considered
- [ ] Performance optimizations applied where needed

## Tools and Extensions

### Recommended VS Code Extensions

- **ES7+ React/Redux/React-Native snippets**
- **TypeScript Importer**
- **Tailwind CSS IntelliSense**
- **Auto Rename Tag**
- **Bracket Pair Colorizer**

### Useful Snippets

```json
// .vscode/settings.json
{
  "typescript.preferences.includePackageJsonAutoImports": "auto",
  "editor.codeActionsOnSave": {
    "source.organizeImports": true
  }
}
```

---

## Backend Development Standards (Go)

### Logging Guidelines

**Philosophy**: Log only what's necessary for debugging critical issues and monitoring production health. Avoid excessive debug logging that clutters logs and impacts performance.

**IMPORTANT**: Always use the imported `slog` package with context methods (`InfoContext`, `ErrorContext`, etc.) for proper context propagation and structured logging.

```go
import (
    "context"
    "log/slog"
)

// ✅ Good: Use slog with context methods
func ProcessImportQueue(ctx context.Context, items []ImportItem) error {
    slog.InfoContext(ctx, "Processing import queue",
        "item_count", len(items))

    for _, item := range items {
        if err := processItem(ctx, item); err != nil {
            slog.ErrorContext(ctx, "Failed to process import item",
                "error", err,
                "item_id", item.ID)
            return err
        }
    }

    return nil
}

// ✅ Good: Log at appropriate levels with context
func StartServer(ctx context.Context, port int) error {
    slog.InfoContext(ctx, "Starting server", "port", port)
    // ... server logic
}

// ✅ Good: Log with structured context from HTTP requests
func HandleWebhook(w http.ResponseWriter, r *http.Request) {
    ctx := r.Context()
    slog.DebugContext(ctx, "Received webhook",
        "method", r.Method,
        "path", r.URL.Path)
    // ... handler logic
}

// ❌ Avoid: Excessive debug logs everywhere
func UpdateConfig(ctx context.Context, cfg Config) error {
    slog.DebugContext(ctx, "UpdateConfig called")
    slog.DebugContext(ctx, "Config value", "config", cfg)
    slog.DebugContext(ctx, "Validating config")

    if err := validateConfig(cfg); err != nil {
        slog.DebugContext(ctx, "Validation failed")
        return err
    }

    slog.DebugContext(ctx, "Saving config")
    if err := saveConfig(cfg); err != nil {
        slog.DebugContext(ctx, "Save failed")
        return err
    }

    slog.DebugContext(ctx, "Config updated successfully")
    return nil
}

// ❌ Avoid: Logging in hot paths or loops
func ProcessFiles(ctx context.Context, files []File) {
    for _, file := range files {
        slog.DebugContext(ctx, "Processing file", "file", file.Name)
        // ... process file
        slog.DebugContext(ctx, "File processed", "file", file.Name)
    }
}

// ❌ Avoid: Using slog without context methods
func BadExample(ctx context.Context) {
    // Don't do this - use InfoContext instead
    slog.Info("Starting operation")
}
```

**Go Logging Guidelines**:

- **Error Level**: Critical errors that require immediate attention
- **Warn Level**: Concerning situations that aren't errors but need monitoring
- **Info Level**: Important business events (server start, config changes, major operations)
- **Debug Level**: Detailed information for troubleshooting (use sparingly)

**Best Practices**:

- **Always use `slog` with context methods**: `InfoContext`, `ErrorContext`, `WarnContext`, `DebugContext`
- Use structured logging with key-value pairs for contextual fields
- Log errors once at the source, not at every layer
- Don't log in tight loops or high-frequency operations
- Include relevant context (IDs, counts, durations) but keep it concise
- Never log sensitive data (passwords, tokens, API keys, PII)
- Use appropriate log levels - don't make everything Info or Debug
- Pass context through function calls to maintain request tracing

**When to Log**:

- ✅ Application lifecycle events (startup, shutdown)
- ✅ Critical errors and failures
- ✅ Important business operations (imports completed, files processed)
- ✅ External service interactions (API calls, webhook receipts)
- ❌ Every function entry/exit
- ❌ Variable values at every step
- ❌ Routine operations in loops
- ❌ Successful validation steps

### API Response Format

All REST API endpoints use a unified response format. Use the response builder functions from `internal/api/response.go`.

**Success Response Format:**

```json
{
  "success": true,
  "data": { ... }
}
```

**Success with Pagination:**

```json
{
  "success": true,
  "data": [ ... ],
  "meta": {
    "total": 100,
    "page": 1,
    "page_size": 20,
    "total_pages": 5
  }
}
```

**Error Response Format:**

```json
{
  "success": false,
  "error": {
    "code": "ERROR_CODE",
    "message": "Human readable message",
    "details": "Additional context or technical details"
  }
}
```

**Response Builder Functions:**

```go
import "github.com/javi11/altmount/internal/api"

// ✅ Good: Use response builders
func (s *Server) handleGetItem(c *fiber.Ctx) error {
    item, err := s.repo.GetItem(c.Context(), id)
    if err != nil {
        return api.RespondInternalError(c, "Failed to get item", err.Error())
    }
    if item == nil {
        return api.RespondNotFound(c, "Item", "")
    }
    return api.RespondSuccess(c, item)
}

// ❌ Avoid: Inline JSON responses
func (s *Server) handleGetItem(c *fiber.Ctx) error {
    item, err := s.repo.GetItem(c.Context(), id)
    if err != nil {
        return c.Status(500).JSON(fiber.Map{
            "success": false,
            "message": "Failed to get item",
        })
    }
    return c.Status(200).JSON(fiber.Map{
        "success": true,
        "data": item,
    })
}
```

**Available Response Builders:**

| Function | HTTP Status | Use Case |
|----------|-------------|----------|
| `RespondSuccess(c, data)` | 200 | Successful response with data |
| `RespondSuccessWithMeta(c, data, meta)` | 200 | Paginated list responses |
| `RespondCreated(c, data)` | 201 | Resource created successfully |
| `RespondNoContent(c)` | 204 | Successful deletion, no body |
| `RespondMessage(c, message)` | 200 | Success with message only |
| `RespondBadRequest(c, message, details)` | 400 | Invalid request syntax |
| `RespondValidationError(c, message, details)` | 400 | Validation failures |
| `RespondUnauthorized(c, message, details)` | 401 | Authentication required |
| `RespondForbidden(c, message, details)` | 403 | Insufficient permissions |
| `RespondNotFound(c, resource, details)` | 404 | Resource not found |
| `RespondConflict(c, message, details)` | 409 | Resource conflict |
| `RespondInternalError(c, message, details)` | 500 | Server errors |
| `RespondServiceUnavailable(c, message, details)` | 503 | Service unavailable |

**Error Codes:**

Standard error codes are defined in `internal/api/errors.go`:

- `BAD_REQUEST` - Invalid request format
- `VALIDATION_ERROR` - Validation failures
- `UNAUTHORIZED` - Authentication required
- `FORBIDDEN` - Insufficient permissions
- `NOT_FOUND` - Resource not found
- `CONFLICT` - Resource conflict
- `INTERNAL_SERVER_ERROR` - Server errors

**Example Handler Migration:**

```go
// Before (inline responses)
func (s *Server) handleDeleteItem(c *fiber.Ctx) error {
    id := c.Params("id")
    if id == "" {
        return c.Status(400).JSON(fiber.Map{
            "success": false,
            "error": fiber.Map{
                "code":    "BAD_REQUEST",
                "message": "Item ID is required",
                "details": "",
            },
        })
    }

    if err := s.repo.Delete(c.Context(), id); err != nil {
        return c.Status(500).JSON(fiber.Map{
            "success": false,
            "error": fiber.Map{
                "code":    "INTERNAL_SERVER_ERROR",
                "message": "Failed to delete item",
                "details": err.Error(),
            },
        })
    }

    return c.SendStatus(204)
}

// After (response builders)
func (s *Server) handleDeleteItem(c *fiber.Ctx) error {
    id := c.Params("id")
    if id == "" {
        return RespondBadRequest(c, "Item ID is required", "")
    }

    if err := s.repo.Delete(c.Context(), id); err != nil {
        return RespondInternalError(c, "Failed to delete item", err.Error())
    }

    return RespondNoContent(c)
}
```

**Note**: SABnzbd API handlers (`sabnzbd_handlers.go`) use a different response format for API compatibility with SABnzbd clients and should NOT use these builders.

---

This document should be updated as the project evolves and new patterns emerge.
