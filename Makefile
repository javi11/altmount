GO ?= go
DOCKER_IMAGE_TAG ?= latest

.DEFAULT_GOAL := check

.PHONY: proto
proto:
	protoc --go_out=./internal/metadata/proto/ internal/metadata/proto/metadata.proto

.PHONY: generate
generate: proto
	go generate ./...

.PHONY: govulncheck
govulncheck:
	go tool govulncheck ./...

.PHONY: tidy go-mod-tidy
tidy: go-mod-tidy
go-mod-tidy:
	$(GO) mod tidy

.PHONY: golangci-lint golangci-lint-fix
golangci-lint-fix: ARGS=--fix
golangci-lint-fix: golangci-lint
golangci-lint:
	go tool golangci-lint run $(ARGS)

.PHONY: junit
junit: | $(JUNIT)
	mkdir -p ./test-results && $(GO) test -v 2>&1 ./... | go tool go-junit-report -set-exit-code > ./test-results/report.xml

.PHONY: coverage
coverage:
	$(GO) test -v -coverprofile=coverage.out ./...

.PHONY: coverage-html
coverage-html: coverage
	$(GO) tool cover -html=coverage.out -o coverage.html

.PHONY: coverage-func
coverage-func: coverage
	$(GO) tool cover -func=coverage.out

.PHONY: coverage-ci
coverage-ci:
	$(GO) test -v -race -coverprofile=coverage.out -covermode=atomic ./...

.PHONY: coverage-total
coverage-total: coverage
	@$(GO) tool cover -func=coverage.out | grep total | awk '{print $$3}' | sed 's/%//'

.PHONY: lint
lint: go-mod-tidy golangci-lint

.PHONY: test test-race
test-race: ARGS=-race
test-race: test
test:
	$(GO) test $(ARGS) ./...

.PHONY: check
check: generate go-mod-tidy golangci-lint test-race

.PHONY: git-hooks
git-hooks:
	@echo '#!/bin/sh\nmake' > .git/hooks/pre-commit
	@chmod +x .git/hooks/pre-commit

.PHONY: docker-build
docker-build:
	docker build -f docker/Dockerfile -t altmount:$(DOCKER_IMAGE_TAG) .

.PHONY: docker-build-ci
docker-build-ci: build-frontend
	docker build -f docker/Dockerfile.ci -t altmount:ci-latest .

.PHONY: build-frontend
build-frontend:
	cd frontend && bun install --frozen-lockfile && bun run build

.PHONY: build-cli
build-cli: build-frontend
	@VERSION=$$(git describe --tags --always --dirty 2>/dev/null || echo "dev"); \
	COMMIT=$$(git rev-parse --short HEAD 2>/dev/null || echo "unknown"); \
	TIMESTAMP=$$(date -u '+%Y-%m-%dT%H:%M:%SZ'); \
	echo "Building altmount CLI (version: $$VERSION, commit: $$COMMIT)..."; \
	CGO_ENABLED=1 $(GO) build \
		-trimpath \
		-tags=cli \
		-ldflags="-s -w -X 'main.Version=$$VERSION' -X 'main.GitCommit=$$COMMIT' -X 'main.Timestamp=$$TIMESTAMP'" \
		-o altmount \
		./cmd/altmount/main.go

.PHONY: build
build: build-cli