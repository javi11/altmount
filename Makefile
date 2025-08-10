GO ?= go

.DEFAULT_GOAL := check

.PHONY: generate
generate:
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

.PHONY: release
release:
	goreleaser --skip-validate --skip-publish --rm-dist

.PHONY: snapshot
snapshot:
	goreleaser --skip-docker --snapshot --skip-publish --rm-dist 

.PHONY: publish
publish:
	goreleaser --rm-dist