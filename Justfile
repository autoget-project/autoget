# List available recipes
default:
    @just --list

# Build backend, organizer and frontend
build: build-backend build-organizer build-frontend

# Build Go backend binary
build-backend:
    cd backend && go build -o bin/autoget ./cmd/main.go

# Build Go organizer binary
build-organizer:
    cd organizer && go build -o bin/organizer ./cmd/server

# Build frontend production bundle
build-frontend:
    cd frontend && pnpm run build

# Build Docker images for backend and organizer
build-images:
    docker build --target deploy -t autoget:latest -f Dockerfile .
    docker build --target deploy -t organizer:latest -f Dockerfile.organizer .

# Run linters for protocol, backend, organizer and frontend
lint: lint-protocol lint-backend lint-organizer lint-frontend

# Lint protocol code using golangci-lint
lint-protocol:
    cd protocol && golangci-lint run ./...

# Lint backend code using golangci-lint
lint-backend:
    cd backend && golangci-lint run ./...

# Lint organizer code using golangci-lint
lint-organizer:
    cd organizer && golangci-lint run

# Lint frontend code using oxlint
lint-frontend:
    cd frontend && pnpm run lint

# Run go mod tidy on protocol, backend and organizer
tidy: tidy-protocol tidy-backend tidy-organizer

# Run go mod tidy on protocol
tidy-protocol:
    cd protocol && go mod tidy

# Run go mod tidy on backend
tidy-backend:
    cd backend && go mod tidy

# Run go mod tidy on organizer
tidy-organizer:
    cd organizer && go mod tidy

# Update go mod dependencies in backend, organizer and protocol
update-go-deps:
    cd protocol && go get -u -t ./...
    cd backend && go get -u -t ./...
    cd organizer && go get -u -t ./...
    @just tidy

# Update pnpm dependencies in frontend
update-pnpm-deps:
    cd frontend && pnpm update

# Update both Go and pnpm dependencies
update-deps: update-go-deps update-pnpm-deps

# Run protocol, backend, organizer and frontend tests
test: test-protocol test-backend test-organizer test-frontend

# Run protocol tests
test-protocol:
    cd protocol && go test -v ./...

# Run backend tests; LOCAL_TEST guards tests that hit live indexer sites,
# which are often blocked on CI runners
test-backend:
    cd backend && LOCAL_TEST=1 go test -v ./...

# Run organizer tests
test-organizer:
    cd organizer && go test -v ./...

# Run frontend tests
test-frontend:
    cd frontend && pnpm run test:run

# Run organizer end-to-end (E2E) tests; hits live LLM providers and costs money
test-e2e:
    cd organizer && E2E_TEST=1 go test -v ./tests/e2e/...

# Run the organizer HTTP service locally
run-organizer:
    cd organizer && go run ./cmd/server

# Format protocol, backend, organizer and frontend code
fmt: fmt-protocol fmt-backend fmt-organizer fmt-frontend

# Format protocol Go code using goimports
fmt-protocol:
    goimports -w -local "github.com/autoget-project/autoget/protocol" protocol

# Format backend Go code using goimports
fmt-backend:
    goimports -w -local "github.com/autoget-project/autoget/backend" backend

# Format organizer Go code using goimports
fmt-organizer:
    goimports -w -local "github.com/autoget-project/autoget/organizer" organizer

# Format frontend code using oxfmt
fmt-frontend:
    cd frontend && pnpm run fmt

# Check formatting without modifying files
fmt-check: fmt-check-protocol fmt-check-backend fmt-check-organizer fmt-check-frontend

# Check protocol Go code formatting using goimports
fmt-check-protocol:
    @test -z "$(goimports -local github.com/autoget-project/autoget/protocol -l protocol)" || (echo "Unformatted Go files found:" && goimports -local github.com/autoget-project/autoget/protocol -l protocol && exit 1)

# Check backend Go code formatting using goimports
fmt-check-backend:
    @test -z "$(goimports -local github.com/autoget-project/autoget/backend -l backend)" || (echo "Unformatted Go files found:" && goimports -local github.com/autoget-project/autoget/backend -l backend && exit 1)

# Check organizer Go code formatting using goimports
fmt-check-organizer:
    @test -z "$(goimports -local github.com/autoget-project/autoget/organizer -l organizer)" || (echo "Unformatted Go files found:" && goimports -local github.com/autoget-project/autoget/organizer -l organizer && exit 1)

# Check frontend code formatting
fmt-check-frontend:
    cd frontend && pnpm run fmt:check

# Run frontend TypeScript type checking
typecheck:
    cd frontend && pnpm run typecheck

# Clean build artifacts
clean:
    rm -rf backend/bin organizer/bin frontend/dist
