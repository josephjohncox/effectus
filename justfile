# Effectus Development Commands

# Variables
DB_DSN := env_var_or_default("DB_DSN", "postgres://effectus:effectus@localhost/effectus_dev?sslmode=disable")
MIGRATIONS_DIR := "migrations"
DOCKER_COMPOSE := "docker-compose -f docker-compose.yml"

# Default recipe
default:
	@just --list

# Install development dependencies
install:
	go mod download
	go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
	go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
	# Install buf if not present
	command -v buf >/dev/null 2>&1 || curl -sSL https://github.com/bufbuild/buf/releases/latest/download/buf-$(uname -s)-$(uname -m) -o /usr/local/bin/buf && chmod +x /usr/local/bin/buf

# Install SQL tooling (sqlc and goose)
install-sql-tools:
	@echo "Installing SQL tooling..."
	go install github.com/sqlc-dev/sqlc/cmd/sqlc@v1.25.0
	go install github.com/pressly/goose/v3/cmd/goose@v3.17.0
	@echo "✅ Tools installed"

# Build the project
build:
	just buf-generate
	go build -o bin/effectusc ./effectus-go/cmd/effectusc
	go build -o bin/effectusd ./effectus-go/cmd/effectusd

# Run all tests
test:
	go test -v ./effectus-go/...

# Run tests with coverage
test-coverage:
	go test -v -coverprofile=coverage.out ./effectus-go/...
	go tool cover -html=coverage.out -o coverage.html

# Lint the codebase
lint:
	golangci-lint run ./effectus-go/...
	just buf-lint

# Format code
fmt:
	go fmt ./effectus-go/...
	just buf-format

# Clean build artifacts
clean:
	rm -rf bin/
	rm -rf effectus-go/gen/
	rm -rf clients/
	rm -f coverage.out coverage.html

# === Buf Commands ===

# Lint protobuf files
buf-lint:
	buf lint

# Format protobuf files
buf-format:
	buf format -w

# Generate code from protobuf definitions
buf-generate:
	buf generate

# Build protobuf modules
buf-build:
	buf build

# Check for breaking changes
buf-breaking:
	buf breaking --against '.git#branch=main'

# Push to buf registry (requires authentication)
buf-push:
	buf push

# === SQL Database Commands ===

# Setup development database with Docker
setup-db:
	@echo "Starting PostgreSQL with Docker..."
	{{DOCKER_COMPOSE}} up -d postgres
	@echo "Waiting for database to be ready..."
	sleep 5
	@echo "✅ Database ready"

# Setup test database
setup-test-db:
	@echo "Creating test database..."
	-createdb effectus_test
	@echo "✅ Test database ready"

# Generate Go code from SQL queries
sql-generate:
	@echo "Generating Go code from SQL queries..."
	cd effectus-go/runtime && sqlc generate
	@echo "✅ Code generated in internal/db/"

# Check if generated code is up to date
sql-generate-check:
	@echo "Checking if generated code is up to date..."
	@git diff --quiet effectus-go/runtime/internal/db/ || (echo "❌ Generated code is out of date. Run 'just sql-generate'" && exit 1)
	@echo "✅ Generated code is up to date"

# Run all pending migrations
migrate-up:
	@echo "Running migrations..."
	cd effectus-go/runtime && goose -dir {{MIGRATIONS_DIR}} postgres "{{DB_DSN}}" up
	@echo "✅ Migrations complete"

# Rollback last migration
migrate-down:
	@echo "Rolling back last migration..."
	cd effectus-go/runtime && goose -dir {{MIGRATIONS_DIR}} postgres "{{DB_DSN}}" down
	@echo "✅ Rollback complete"

# Show migration status
migrate-status:
	@echo "Migration status:"
	cd effectus-go/runtime && goose -dir {{MIGRATIONS_DIR}} postgres "{{DB_DSN}}" status

# Show current migration version
migrate-version:
	@echo "Current migration version:"
	cd effectus-go/runtime && goose -dir {{MIGRATIONS_DIR}} postgres "{{DB_DSN}}" version

# Create a new migration
migrate-create name:
	@echo "Creating migration: {{name}}"
	cd effectus-go/runtime && goose -dir {{MIGRATIONS_DIR}} create {{name}} sql
	@echo "✅ Migration created"

# Reset database (⚠️ DESTROYS ALL DATA)
migrate-reset:
	@echo "⚠️  This will destroy all data. Continue? (Press Enter to continue, Ctrl+C to cancel)"
	@read
	@echo "Resetting database..."
	cd effectus-go/runtime && goose -dir {{MIGRATIONS_DIR}} postgres "{{DB_DSN}}" reset
	@echo "✅ Database reset"

# Reset and run all migrations (⚠️ DESTROYS ALL DATA)  
migrate-fresh:
	@echo "⚠️  This will destroy all data. Continue? (Press Enter to continue, Ctrl+C to cancel)"
	@read
	@echo "Fresh migration..."
	cd effectus-go/runtime && goose -dir {{MIGRATIONS_DIR}} postgres "{{DB_DSN}}" reset
	cd effectus-go/runtime && goose -dir {{MIGRATIONS_DIR}} postgres "{{DB_DSN}}" up
	@echo "✅ Fresh migration complete"

# Run integration tests with database
test-integration: setup-test-db
	@echo "Running integration tests..."
	DB_DSN="postgres://effectus:effectus@localhost/effectus_test?sslmode=disable" go test -v -tags=integration ./effectus-go/runtime/...

# Test migrations up and down
test-migrate:
	@echo "Testing migrations..."
	cd effectus-go/runtime && goose -dir {{MIGRATIONS_DIR}} postgres "{{DB_DSN}}" up
	cd effectus-go/runtime && goose -dir {{MIGRATIONS_DIR}} postgres "{{DB_DSN}}" reset
	cd effectus-go/runtime && goose -dir {{MIGRATIONS_DIR}} postgres "{{DB_DSN}}" up
	@echo "✅ Migration tests complete"

# Complete development setup for SQL
dev-sql-setup: install-sql-tools setup-db migrate-up sql-generate
	@echo "✅ SQL development environment ready!"

# Reset SQL development environment
dev-sql-reset: migrate-fresh sql-generate
	@echo "✅ SQL development environment reset!"

# Validate all SQL and generated code
sql-validate: sql-generate-check
	@echo "Validating SQL queries..."
	cd effectus-go/runtime && sqlc vet
	@echo "✅ Validation complete"

# Lint SQL files (requires sqlfluff)
sql-lint:
	@echo "Linting SQL files..."
	@if command -v sqlfluff >/dev/null 2>&1; then sqlfluff lint {{MIGRATIONS_DIR}}; else echo "⚠️  sqlfluff not installed. Install with: pip install sqlfluff"; fi

# Format SQL files (requires sqlfluff)
sql-format:
	@echo "Formatting SQL files..."
	@if command -v sqlfluff >/dev/null 2>&1; then sqlfluff format {{MIGRATIONS_DIR}} --dialect postgres; else echo "⚠️  sqlfluff not installed. Install with: pip install sqlfluff"; fi

# Generate schema documentation
schema-docs:
	@echo "Generating schema documentation..."
	@echo "Database Schema Documentation" > effectus-go/runtime/SCHEMA.md
	@echo "============================" >> effectus-go/runtime/SCHEMA.md
	@psql "{{DB_DSN}}" -c "\dt" >> effectus-go/runtime/SCHEMA.md
	@echo "✅ Schema documentation generated"

# Clean generated SQL files
sql-clean:
	@echo "Cleaning generated SQL files..."
	rm -rf effectus-go/runtime/internal/db/*.go
	@echo "✅ SQL clean complete"

# Clean everything including database (⚠️ DESTROYS ALL DATA)
sql-clean-all: sql-clean
	@echo "⚠️  This will destroy database. Continue? (Press Enter to continue, Ctrl+C to cancel)"
	@read
	{{DOCKER_COMPOSE}} down -v postgres
	@echo "✅ Complete SQL cleanup done"

# Open database shell
db-shell:
	@echo "Opening database shell..."
	psql "{{DB_DSN}}"

# Dump database schema and data
db-dump:
	@echo "Dumping database..."
	pg_dump "{{DB_DSN}}" > effectus_dump_$(date +%Y%m%d_%H%M%S).sql
	@echo "✅ Database dumped"

# Restore database from dump
db-restore dump:
	@echo "⚠️  This will overwrite the database. Continue? (Press Enter to continue, Ctrl+C to cancel)"
	@read
	@echo "Restoring database from {{dump}}..."
	psql "{{DB_DSN}}" < {{dump}}
	@echo "✅ Database restored"

# === VS Code Extension Commands ===

# Install VS Code extension dependencies
vscode-install:
	@echo "Installing VS Code extension dependencies..."
	cd tools/vscode-extension && npm install
	@echo "✅ VS Code extension dependencies installed"

# Compile TypeScript for VS Code extension  
vscode-compile:
	@echo "Compiling VS Code extension..."
	cd tools/vscode-extension && npm run compile
	@echo "✅ VS Code extension compiled"

# Watch mode for VS Code extension development
vscode-watch:
	@echo "Starting VS Code extension watch mode..."
	cd tools/vscode-extension && npm run watch

# Package VS Code extension
vscode-package:
	@echo "Packaging VS Code extension..."
	cd tools/vscode-extension && npm run package
	@echo "✅ VS Code extension packaged as .vsix file"

# Install packaged VS Code extension locally
vscode-install-local:
	@echo "Installing VS Code extension locally..."
	cd tools/vscode-extension && code --install-extension effectus-language-support-*.vsix
	@echo "✅ VS Code extension installed locally"

# Lint VS Code extension
vscode-lint:
	@echo "Linting VS Code extension..."
	cd tools/vscode-extension && npm run lint

# Test VS Code extension
vscode-test:
	@echo "Testing VS Code extension..."
	cd tools/vscode-extension && npm run test

# Complete VS Code extension development setup
vscode-dev-setup: vscode-install vscode-compile
	@echo "✅ VS Code extension development environment ready!"
	@echo "Use 'just vscode-watch' for development"
	@echo "Use 'just vscode-package' to create .vsix file"

# === Schema Management ===

# Register a new verb schema
register-verb name input_schema output_schema:
	go run ./effectus-go/cmd/effectusc schema register-verb --name={{name}} --input="{{input_schema}}" --output="{{output_schema}}"

# Register a new fact schema
register-fact name schema:
	go run ./effectus-go/cmd/effectusc schema register-fact --name={{name}} --schema="{{schema}}"

# List all registered schemas
list-schemas:
	go run ./effectus-go/cmd/effectusc schema list

# Validate schema compatibility
validate-schemas:
	go run ./effectus-go/cmd/effectusc schema validate

# Generate client code for all languages
generate-clients:
	just buf-generate
	@echo "Generated clients for Go, Python, TypeScript, Java, and Rust"

# === Development Workflow ===

# Complete development workflow: format, lint, test, build
dev:
	just fmt
	just lint
	just test
	just build

# Complete development workflow with SQL and VS Code extension
dev-full: dev dev-sql-setup vscode-dev-setup
	@echo "✅ Complete development environment ready!"

# Watch for changes and rebuild (requires entr)
watch:
	find . -name "*.go" -o -name "*.proto" -o -name "*.sql" | entr -r just dev

# Start development server
serve:
	go run ./effectus-go/cmd/effectusd

# === Docker Commands ===

# Build Docker image
docker-build:
	docker build -t effectus:latest .

# Run in Docker
docker-run:
	docker run -p 8080:8080 effectus:latest

# === Examples ===

# Run the coherent flow example
example-coherent-flow:
	cd examples/coherent_flow && go run main.go

# Run the extension system example
example-extension-system:
	cd examples/extension_system && go run main.go

# Run the gRPC execution example
example-grpc-execution:
	cd examples/grpc_execution && go run main.go

# Run the modern SQL usage example
example-modern-sql:
	cd examples/modern_sql_usage && go run main.go

# === Documentation ===

# Generate documentation
docs:
	go doc -all ./effectus-go/... > docs/api.md

# Serve documentation locally
docs-serve:
	cd docs && python3 -m http.server 8000

# === Release ===

# Prepare release (bump version, tag, push)
release version:
	git tag v{{version}}
	git push origin v{{version}}
	just buf-push

# Create release binaries
release-build:
	GOOS=linux GOARCH=amd64 go build -o bin/effectusc-linux-amd64 ./effectus-go/cmd/effectusc
	GOOS=darwin GOARCH=amd64 go build -o bin/effectusc-darwin-amd64 ./effectus-go/cmd/effectusc
	GOOS=windows GOARCH=amd64 go build -o bin/effectusc-windows-amd64.exe ./effectus-go/cmd/effectusc