# Effectus development commands. Use `just --list` for the supported surface.

DB_DSN := env_var_or_default("DB_DSN", "postgres://effectus:effectus@localhost:55433/effectus_saga?sslmode=disable")
COMPOSE := "docker compose -f tests/fixtures/durable-stack/docker-compose.yml"

# List supported workflows.
default:
	@just --list

# Download Go dependencies and install the pinned generation tools.
install:
	go mod download
	just _generate-tools

[private]
_generate-tools:
	@set -eu; mkdir -p .tools/bin; GOBIN="$PWD/.tools/bin" go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.36.11; GOBIN="$PWD/.tools/bin" go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.6.0; GOBIN="$PWD/.tools/bin" go install github.com/bufbuild/buf/cmd/buf@v1.50.0; GOBIN="$PWD/.tools/bin" go install github.com/sqlc-dev/sqlc/cmd/sqlc@v1.29.0

# Build generated code and both command-line programs.
build: _generate-tools
	PATH="$PWD/.tools/bin:$PATH" buf generate --template buf.gen.go.yaml
	cd runtime && PATH="$OLDPWD/.tools/bin:$PATH" sqlc generate
	go build -o bin/effectusc ./cmd/effectusc
	go build -o bin/effectusd ./cmd/effectusd

# Run root-module tests.
test:
	go test ./...

# Run tests in every reviewed Go module.
test-modules:
	@set -eu; go run ./internal/guardrails/cmd modules | while IFS= read -r module; do echo "==> $module"; (cd "$module" && go test ./...); done

# Run the three golden examples and their shared scenario tests.
test-examples:
	go test ./internal/demo/orderreview
	go -C examples run ./embedded_orders
	go test ./examples/grpc_execution

# Verify frozen v0.3 compatibility imports from a released root module on the Go proxy.
smoke-compat version:
	scripts/compat-proxy-smoke.sh "{{version}}"

# Check reviewed inventories, public API boundaries, docs claims, and dependency rules.
guardrails:
	go test ./internal/guardrails/... ./cmd/effectusc ./cmd/effectusd -run 'Guardrail|Inventory|Published|Documented|Compatibility|Deprecation|Dependency|Discover|Recipe|PublicAPI|PublicPackages'
	go run ./internal/guardrails/cmd check

# Format Go and protobuf sources.
fmt:
	go fmt ./...
	buf format -w

# Run static checks.
lint:
	golangci-lint run ./...
	buf lint

# Remove local build output only.
clean:
	rm -rf bin out coverage.out coverage.html

# Start the test-only PostgreSQL fixture without removing existing data.
setup-db:
	{{COMPOSE}} up -d postgres
	@for attempt in $(seq 1 60); do {{COMPOSE}} exec -T postgres pg_isready -U effectus -d effectus_saga >/dev/null 2>&1 && exit 0; sleep 1; done; echo "ERROR PostgreSQL did not become ready"; exit 1

# Run durable PostgreSQL integration tests; DB_DSN must point at an explicit database.
test-integration:
	@test -n "${DB_DSN:-}" || { echo "ERROR DB_DSN is required"; exit 1; }
	EFFECTUS_POSTGRES_DSN="{{DB_DSN}}" go run ./cmd/effectusd --database-migrations=apply
	DB_DSN="{{DB_DSN}}" POSTGRES_DSN="{{DB_DSN}}" go test -p 1 -tags=integration ./runtime/... ./schema ./cmd/effectusd

# Build the documentation site strictly.
docs:
	NO_MKDOCS_2_WARNING=true mkdocs build --strict

# Serve the documentation site.
docs-serve:
	NO_MKDOCS_2_WARNING=true mkdocs serve

# Format, lint, test, and build.
dev:
	just fmt
	just lint
	just test
	just build

# Lint the VS Code extension.
vscode-lint:
	cd tools/vscode-extension && npm ci && npm run lint

# Test the VS Code extension.
vscode-test:
	cd tools/vscode-extension && npm ci && npm test
