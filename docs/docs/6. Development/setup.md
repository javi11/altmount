# Development Setup

This guide will help you set up a development environment for AltMount.

## Prerequisites

Before you begin, ensure you have the following installed on your system:

- **Go 1.26.0+** - [Download Go](https://golang.org/dl/)
- **Bun** - [Install Bun](https://bun.sh/docs/installation)
- **Protobuf Compiler** - [Install Protocol Buffers](https://grpc.io/docs/protoc-installation/)
- **Git** - [Download Git](https://git-scm.com/downloads)

### Optional Tools

- **Docker** - For containerized development
- **golangci-lint** - For Go linting (installed via `make tidy`)

## Project Structure

```
altmount/
├── cmd/altmount/          # Main application entry point
├── internal/              # Internal Go packages
├── frontend/              # React frontend application
├── docs/                  # Documentation
├── docker/                # Docker configuration
├── example/               # Example configuration and data
└── pkg/                   # Public Go packages
```

## Backend Development

### 1. Clone the Repository

```bash
git clone https://github.com/javi11/altmount.git
cd altmount
```

### 2. Install Dependencies

```bash
make tidy
```

### 3. Generate Code

Before running the application, you need to generate some code:

```bash
make generate
```

This command will:

- Generate protobuf code
- Run `go generate` to create any necessary generated files

### 4. Run the Server

To start the AltMount server in development mode:

```bash
make
go run ./cmd/altmount serve --config=./config.yaml
```

The server will start on the default port (8080) and you can access the web interface at `http://localhost:8080`.

### 5. Development Commands

The project includes several useful Make targets for development:

```bash
# Run all checks (linting, tests, etc.)
make check

# Run tests
make test

# Run tests with race detection
make test-race

# Run linting
make lint

# Generate code
make generate

# Run vulnerability checks
make govulncheck

# Build the application
make build

# Clean up generated files
make clean
```

## Frontend Development

### 1. Navigate to Frontend Directory

```bash
cd frontend
```

### 2. Install Dependencies

```bash
bun i
```

### 3. Start Development Server

```bash
bun dev
```

The frontend development server will start on `http://localhost:5173` (or another available port) with hot reloading enabled.

### 4. Frontend Development Commands

```bash
# Start development server
bun dev

# Build for production
bun run build

# Preview production build
bun run preview

# Run linting
bun run lint

# Run type checking and linting
bun run check
```

## Configuration

### Backend Configuration

Create a `config.yaml` file in the project root. You can use the provided sample configuration:

```bash
cp config.sample.yaml config.yaml
```

Edit the configuration file to match your development environment:

```yaml
# Example configuration
server:
  port: 8080
  host: "localhost"

database:
  path: "./altmount.db"
# Add your specific configuration here
```

### Frontend Configuration

The frontend automatically connects to the backend running on `localhost:8080` in development mode. If you need to change this, modify the API base URL in `frontend/src/api/client.ts`.

## Running Both Services

For full development, you'll need both the backend and frontend running:

### Terminal 1 - Backend

```bash
# In project root
make
go run ./cmd/altmount serve --config=./config.yaml
```

### Terminal 2 - Frontend

```bash
# In frontend directory
cd frontend
bun install
bun dev
```

## Testing

### Backend Tests

```bash
# Run all tests
make test

# Run tests with coverage
make coverage

# Run tests with race detection
make test-race

# View coverage report in browser
make coverage-html
```

### Frontend Tests

```bash
cd frontend
bun test
```

## Linting and Code Quality

### Backend Linting

```bash
# Run all linting checks
make lint

# Fix auto-fixable issues
make golangci-lint-fix
```

### Frontend Linting

```bash
cd frontend
bun run lint
bun run check
```

## Building for Production

### Backend

```bash
# Build binary
go build -o altmount ./cmd/altmount

# Or use the Makefile
make build
```

### Frontend

```bash
cd frontend
bun run build
```

The built frontend files will be in the `frontend/dist` directory.

## Docker Development

If you prefer to develop using Docker:

```bash
# Build the development image
docker build -f docker/Dockerfile -t altmount:dev .

# Run the container
docker run -p 8080:8080 -v $(pwd)/config.yaml:/app/config.yaml altmount:dev
```

## Troubleshooting

### Common Issues

1. **Protobuf compilation errors**: Ensure you have `protoc` installed and in your PATH
2. **Go module issues**: Run `go mod tidy` to clean up dependencies
3. **Frontend build errors**: Delete `node_modules` and run `bun install` again
4. **Port conflicts**: Change the port in your configuration file

### Getting Help

- Check the [Troubleshooting Guide](../5. Troubleshooting/common-issues.md)
- Review the [API Documentation](../4. API/endpoints.md)
- Open an issue on [GitHub](https://github.com/javi11/altmount/issues)

## Contributing

When contributing to AltMount:

1. Fork the repository
2. Create a feature branch
3. Make your changes
4. Run `make check` to ensure all tests pass
5. Run `make lint` to ensure code quality
6. Submit a pull request
