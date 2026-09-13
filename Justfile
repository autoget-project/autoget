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
    docker build -t autoget:latest -f Dockerfile .
    docker build -t organizer:latest -f Dockerfile.organizer .

# Run linters for backend, organizer and frontend
lint: lint-backend lint-organizer lint-frontend

# Lint backend code using golangci-lint
lint-backend:
    cd backend && golangci-lint run ./...

# Lint organizer code using golangci-lint
lint-organizer:
    cd organizer && golangci-lint run

# Lint frontend code using oxlint
lint-frontend:
    cd frontend && pnpm run lint

# Run go mod tidy on backend and organizer
tidy: tidy-backend tidy-organizer

# Run go mod tidy on backend
tidy-backend:
    cd backend && go mod tidy

# Run go mod tidy on organizer
tidy-organizer:
    cd organizer && go mod tidy

# Update go mod dependencies in backend and organizer
update-go-deps:
    cd backend && go get -u -t ./...
    cd organizer && go get -u -t ./...
    @just tidy

# Update pnpm dependencies in frontend
update-pnpm-deps:
    cd frontend && pnpm update

# Update both Go and pnpm dependencies
update-deps: update-go-deps update-pnpm-deps

# Run backend, organizer and frontend tests
test: test-backend test-organizer test-frontend

# Run backend tests
test-backend:
    cd backend && go test -v ./...

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

# Format backend, organizer and frontend code
fmt: fmt-backend fmt-organizer fmt-frontend

# Format backend Go code using goimports
fmt-backend:
    goimports -w -local "github.com/autoget-project/autoget/backend" backend

# Format organizer Go code using goimports
fmt-organizer:
    goimports -w -local "github.com/autoget-project/organizer" organizer

# Format frontend code using oxfmt
fmt-frontend:
    cd frontend && pnpm run fmt

# Check formatting without modifying files
fmt-check: fmt-check-backend fmt-check-organizer fmt-check-frontend

# Check backend Go code formatting using goimports
fmt-check-backend:
    @test -z "$(goimports -local github.com/autoget-project/autoget/backend -l backend)" || (echo "Unformatted Go files found:" && goimports -local github.com/autoget-project/autoget/backend -l backend && exit 1)

# Check organizer Go code formatting using goimports
fmt-check-organizer:
    @test -z "$(goimports -local github.com/autoget-project/organizer -l organizer)" || (echo "Unformatted Go files found:" && goimports -local github.com/autoget-project/organizer -l organizer && exit 1)

# Check frontend code formatting
fmt-check-frontend:
    cd frontend && pnpm run fmt:check

# Run frontend TypeScript type checking
typecheck:
    cd frontend && pnpm run typecheck

# Clean build artifacts
clean:
    rm -rf backend/bin organizer/bin frontend/dist
