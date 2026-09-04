---
title: Development Setup
description: Set up a local development environment for contributing to AltMount's Go backend and React frontend.
keywords: [altmount, development, setup, contributing, go, react, local environment]
---

# Development Setup

This guide will help you set up a development environment for AltMount.

## Prerequisites

Before you begin, ensure you have the following installed on your system:

- **Go 1.24.5+** - [Download Go](https://golang.org/dl/)
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

### Streaming Benchmarks

Changes to the read path (`internal/usenet`, `internal/nzbfilesystem`, `internal/pool`) are gated by a reproducible streaming benchmark. It runs the real reader stack against an in-process Usenet provider simulator (`internal/testsupport/nntpserver`) that models round-trip time, per-connection bandwidth and aggregate bandwidth, so results do not depend on your provider or network.

```bash
# Run every scenario (about 7-10 minutes) and write bench/results/<short-sha>.json
make bench-stream

# Fail if any gated metric regressed against a stored result
make bench-compare BASE=baseline-main
```

Run it on an idle machine: the simulator is CPU-bound at high aggregate bandwidth and a busy laptop shifts throughput figures by several percent.

#### Provider profiles

| Profile        | RTT    | Per connection | Aggregate | Connections | Models                        |
| -------------- | ------ | -------------- | --------- | ----------- | ----------------------------- |
| `premium-750k` | 40 ms  | 8 MB/s         | 400 MB/s  | 50          | Fast provider, 750 KB articles |
| `slow-4m`      | 100 ms | 3 MB/s         | 60 MB/s   | 20          | Slow provider, 4 MiB articles  |

#### Scenarios

| ID  | Benchmark                        | What it measures                                                                   |
| --- | -------------------------------- | ---------------------------------------------------------------------------------- |
| B1  | `BenchmarkStreamColdOpen`        | Time to first byte on a fresh open, and articles fetched to get there              |
| B2  | `BenchmarkStreamSequential`      | Startup time, steady-state throughput after 16 MB, and bytes fetched vs. delivered |
| B3  | `BenchmarkStreamSeekStorm`       | Random 64 KB reads across a file: latency and articles per read                    |
| B4  | `BenchmarkStreamFourConcurrent`  | Four players at once: slowest and fastest stream, stall p99                        |
| B5  | `BenchmarkStreamTwoHandles`      | Two handles on one file: duplicate article downloads                               |
| B6  | `BenchmarkStreamUnderContention` | One stream while an import saturates the pool: stream and import throughput        |
| B7  | `BenchmarkStreamFailover`        | Reads that hit a missing article on the first provider: TTFB and bodies per miss   |
| B8  | `BenchmarkStreamPauseResume`     | Bytes fetched while the player is paused                                           |
| B9  | `BenchmarkStreamForwardSkip`     | Jump 100 MB ahead: read latency and articles fetched                               |
| B10 | `BenchmarkStreamReplay`          | Close and reopen the same file: bytes refetched and replay read time               |
| B11 | `BenchmarkStreamSeekAndResume`   | Five 500 MB jumps: seek read time and total bytes fetched                          |

#### How the gate works

- Each scenario runs three times (`ALTMOUNT_BENCH_REPS`) and records the median, which removes most run-to-run noise.
- A metric fails when it moves more than 5 % in its bad direction. Noisy metrics carry a wider per-metric tolerance (for example 15 % on stream throughput under contention).
- Tail metrics (p99, per-scenario article counts) are informational: they print in the table but never fail the gate.
- `bench/results/` holds one committed result per landed change (`baseline-main.json`, `phase1-...json`, ...). Per-commit files named by short SHA are scratch output and are not committed.

#### Runtime A/B without code changes

The benchmark binary is a normal Go test, so runtime knobs apply to it. To measure a GC or memory setting, run with the environment variable and compare against the last committed result:

```bash
GOMEMLIMIT=512MiB ALTMOUNT_BENCH_OUT=$PWD/bench/results/memlimit.json \
  go test ./internal/nzbfilesystem/ -run '^$' -bench 'BenchmarkStream' -benchtime 1x -timeout 60m
make bench-compare BASE=phase9-housekeeping BENCH_OUT=bench/results/memlimit.json
```

#### Adding a scenario

Scenarios live in `internal/nzbfilesystem/stream_bench_test.go` and share the harness in `stream_bench_harness_test.go`. Sample the new scenario on the base branch first and commit that result, so the PR that changes behaviour has a before number. Mark tails and counts with `info(...)` and give inherently noisy metrics a `Tolerance`.

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
