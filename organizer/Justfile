# AutoGet Organizer Quality Gates & Tasks

# Default recipe: show help summary
default:
	@just --list

# Quality Gate: compile/build the project
build:
	go build -o build/ ./...

# Build Docker container image
build-image tag="ghcr.io/autoget-project/organizer:latest":
	docker build -t {{tag}} .

# Quality Gate: execute unit/integration tests
test:
	go test ./...

# Quality Gate: format source code using goimports
fmt:
	goimports -w -local github.com/autoget-project/organizer .

# Quality Gate: run static analysis and linting using golangci-lint
lint:
	golangci-lint run

# Run end-to-end (E2E) tests
test-e2e:
	E2E_TEST=1 go test -v ./tests/e2e/...

# Alias for the end-to-end suite
e2e: test-e2e

# Run tests for one package, e.g. just test-agent internal/pipeline/stage2_enricher
test-agent target:
	go test -v ./{{target}}/...

# Run the HTTP service
run:
	go run ./cmd/server
