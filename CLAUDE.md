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

```
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
1. **Run code quality checks**: `bun run check` (linting, formatting, and code quality)
2. **Test build**: `bun run build` (TypeScript compilation + Vite build)
3. **Review changes**: Ensure code follows these standards

Note: Use `bun run lint` for linting-only checks when needed.

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

This document should be updated as the project evolves and new patterns emerge.