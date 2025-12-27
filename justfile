# Effectus Development Commands

# Variables
DB_DSN := env_var_or_default("DB_DSN", "postgres://effectus:effectus@localhost/effectus_dev?sslmode=disable")
MIGRATIONS_DIR := "migrations"
DOCKER_COMPOSE := "docker-compose -f docker-compose.yml"
WAREHOUSE_DEVSTACK := "examples/warehouse_sources/devstack"
CDC_STACK := "examples/cdc_stack"
UI_DEMO_RULES := "examples/fraud_e2e/rules"
UI_DEMO_SCHEMA := "examples/fraud_e2e/schema"
UI_DEMO_VERBS := "examples/fraud_e2e/schema/fraud_verbs.json"
UI_DEMO_VERB_DIR := "examples/fraud_e2e/verbs"
UI_DEMO_BUNDLE := "out/ui_demo/bundle.json"
UI_DEMO_FACTS := "examples/fraud_e2e/data/facts_payload.json"
UI_DEMO_TOKEN := "demo-token"
UI_FLOW_DEMO_RULES := "examples/flow_ui_demo/rules"
UI_FLOW_DEMO_SCHEMA := "examples/flow_ui_demo/schema"
UI_FLOW_DEMO_VERBS := "examples/flow_ui_demo/schema/flow_verbs.json"
UI_FLOW_DEMO_VERB_DIR := "examples/flow_ui_demo/verbs"
UI_FLOW_DEMO_BUNDLE := "out/flow_ui_demo/bundle.json"
UI_FLOW_DEMO_FACTS := "examples/flow_ui_demo/data/facts_payload.json"
UI_FLOW_DEMO_STREAM := "examples/flow_ui_demo/scripts/stream_facts.sh"
UI_FLOW_DEMO_TOKEN := "flow-demo-token"

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
	@echo "OK Tools installed"

# Build the project
build:
	just buf-generate
	go build -o bin/effectusc ./cmd/effectusc
	go build -o bin/effectusd ./cmd/effectusd

# Run all tests
test:
	go test -v ./...

# Run tests with coverage
test-coverage:
	go test -v -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html

# Lint the codebase
lint:
	golangci-lint run ./...
	just buf-lint

# Format code
fmt:
	go fmt ./...
	just buf-format

# Clean build artifacts
clean:
	rm -rf bin/
	rm -rf gen/
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

# Generate proto docs (optional; requires doc plugin)
buf-generate-docs:
	buf generate --template buf.gen.docs.yaml

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
	@echo "OK Database ready"

# Setup test database
setup-test-db:
	@echo "Creating test database..."
	-createdb effectus_test
	@echo "OK Test database ready"

# Generate Go code from SQL queries
sql-generate:
	@echo "Generating Go code from SQL queries..."
	cd runtime && sqlc generate
	@echo "OK Code generated in internal/db/"

# Check if generated code is up to date
sql-generate-check:
	@echo "Checking if generated code is up to date..."
	@git diff --quiet runtime/internal/db/ || (echo "ERROR Generated code is out of date. Run 'just sql-generate'" && exit 1)
	@echo "OK Generated code is up to date"

# Run all pending migrations
migrate-up:
	@echo "Running migrations..."
	cd runtime && goose -dir {{MIGRATIONS_DIR}} postgres "{{DB_DSN}}" up
	@echo "OK Migrations complete"

# Rollback last migration
migrate-down:
	@echo "Rolling back last migration..."
	cd runtime && goose -dir {{MIGRATIONS_DIR}} postgres "{{DB_DSN}}" down
	@echo "OK Rollback complete"

# Show migration status
migrate-status:
	@echo "Migration status:"
	cd runtime && goose -dir {{MIGRATIONS_DIR}} postgres "{{DB_DSN}}" status

# Show current migration version
migrate-version:
	@echo "Current migration version:"
	cd runtime && goose -dir {{MIGRATIONS_DIR}} postgres "{{DB_DSN}}" version

# Create a new migration
migrate-create name:
	@echo "Creating migration: {{name}}"
	cd runtime && goose -dir {{MIGRATIONS_DIR}} create {{name}} sql
	@echo "OK Migration created"

# Reset database (WARN DESTROYS ALL DATA)
migrate-reset:
	@echo "WARN  This will destroy all data. Continue? (Press Enter to continue, Ctrl+C to cancel)"
	@read
	@echo "Resetting database..."
	cd runtime && goose -dir {{MIGRATIONS_DIR}} postgres "{{DB_DSN}}" reset
	@echo "OK Database reset"

# Reset and run all migrations (WARN DESTROYS ALL DATA)  
migrate-fresh:
	@echo "WARN  This will destroy all data. Continue? (Press Enter to continue, Ctrl+C to cancel)"
	@read
	@echo "Fresh migration..."
	cd runtime && goose -dir {{MIGRATIONS_DIR}} postgres "{{DB_DSN}}" reset
	cd runtime && goose -dir {{MIGRATIONS_DIR}} postgres "{{DB_DSN}}" up
	@echo "OK Fresh migration complete"

# Run integration tests with database
test-integration: setup-test-db
	@echo "Running integration tests..."
	DB_DSN="postgres://effectus:effectus@localhost/effectus_test?sslmode=disable" go test -v -tags=integration ./runtime/...

# === UI Demo ===

# Build a demo bundle (fraud rules) and start the status UI/runtime.
ui-demo:
	@mkdir -p out/ui_demo
	go run ./cmd/effectusc bundle \
		--name fraud-ui-demo \
		--version 1.0.0 \
		--schema-dir {{UI_DEMO_SCHEMA}} \
		--verb-dir {{UI_DEMO_VERB_DIR}} \
		--verbschema {{UI_DEMO_VERBS}} \
		--rules-dir {{UI_DEMO_RULES}} \
		--output {{UI_DEMO_BUNDLE}}
	@echo "Starting effectusd UI..."
	@echo "Token: {{UI_DEMO_TOKEN}}"
	@echo "Open http://localhost:8080/ui"
	@echo ""
	@echo "Example ingest (new facts):"
	@echo "curl -X POST http://localhost:8080/api/facts \\"
	@echo "  -H \"Authorization: Bearer {{UI_DEMO_TOKEN}}\" \\"
	@echo "  -H \"Content-Type: application/json\" \\"
	@echo "  -d @{{UI_DEMO_FACTS}}"
	@echo ""
	@echo "Example dry run (use stored facts):"
	@echo "curl -X POST http://localhost:8080/api/playground/dry-run \\"
	@echo "  -H \"Authorization: Bearer {{UI_DEMO_TOKEN}}\" \\"
	@echo "  -H \"Content-Type: application/json\" \\"
	@echo "  -d '{\"universe\":\"default\",\"mode\":\"both\",\"use_stored\":true}'"
	go run ./cmd/effectusd \
		--bundle {{UI_DEMO_BUNDLE}} \
		--http-addr :8080 \
		--api-token {{UI_DEMO_TOKEN}} \
		--verb-dir {{UI_DEMO_VERB_DIR}} \
		--facts-store file \
		--facts-path out/ui_demo/facts.json

# Seed the demo facts into the running UI instance.
ui-demo-seed:
	curl -X POST http://localhost:8080/api/facts \
		-H "Authorization: Bearer {{UI_DEMO_TOKEN}}" \
		-H "Content-Type: application/json" \
		-d @{{UI_DEMO_FACTS}}

# Open the demo UI in a browser (macOS/Linux).
ui-demo-open:
	@if command -v open >/dev/null 2>&1; then open http://localhost:8080/ui; \
	elif command -v xdg-open >/dev/null 2>&1; then xdg-open http://localhost:8080/ui; \
	else echo "Open http://localhost:8080/ui"; fi

# Clean demo artifacts (stop the running process with Ctrl+C in its terminal).
ui-demo-down:
	@echo "Stopping UI demo... (use Ctrl+C in the ui-demo terminal if it's running)"
	@rm -rf out/ui_demo

# === UI Flow Demo ===

# Build a flow-heavy demo bundle and start the status UI/runtime.
ui-flow-demo:
	@mkdir -p out/flow_ui_demo
	go run ./cmd/effectusc bundle \
		--name flow-ui-demo \
		--version 1.0.0 \
		--schema-dir {{UI_FLOW_DEMO_SCHEMA}} \
		--verb-dir {{UI_FLOW_DEMO_VERB_DIR}} \
		--verbschema {{UI_FLOW_DEMO_VERBS}} \
		--rules-dir {{UI_FLOW_DEMO_RULES}} \
		--output {{UI_FLOW_DEMO_BUNDLE}}
	@echo "Starting effectusd UI..."
	@echo "Token: {{UI_FLOW_DEMO_TOKEN}}"
	@echo "Open http://localhost:8080/ui"
	@echo "Saga compensation enabled (inverse verbs in {{UI_FLOW_DEMO_VERB_DIR}})"
	@echo ""
	@echo "Example ingest (baseline facts):"
	@echo "curl -X POST http://localhost:8080/api/facts \\"
	@echo "  -H \"Authorization: Bearer {{UI_FLOW_DEMO_TOKEN}}\" \\"
	@echo "  -H \"Content-Type: application/json\" \\"
	@echo "  -d @{{UI_FLOW_DEMO_FACTS}}"
	@echo ""
	@echo "Example dry run (use stored facts):"
	@echo "curl -X POST http://localhost:8080/api/playground/dry-run \\"
	@echo "  -H \"Authorization: Bearer {{UI_FLOW_DEMO_TOKEN}}\" \\"
	@echo "  -H \"Content-Type: application/json\" \\"
	@echo "  -d '{\"universe\":\"default\",\"mode\":\"flow\",\"use_stored\":true}'"
	@echo ""
	@echo "Streaming facts (simulate updates):"
	@echo "{{UI_FLOW_DEMO_STREAM}}"
	go run ./cmd/effectusd \
		--bundle {{UI_FLOW_DEMO_BUNDLE}} \
		--http-addr :8080 \
		--api-token {{UI_FLOW_DEMO_TOKEN}} \
		--verb-dir {{UI_FLOW_DEMO_VERB_DIR}} \
		--saga \
		--facts-store file \
		--facts-path out/flow_ui_demo/facts.json

# Seed the flow demo facts into the running UI instance.
ui-flow-demo-seed:
	curl -X POST http://localhost:8080/api/facts \
		-H "Authorization: Bearer {{UI_FLOW_DEMO_TOKEN}}" \
		-H "Content-Type: application/json" \
		-d @{{UI_FLOW_DEMO_FACTS}}

# Stream fact updates (simulated streaming sources).
ui-flow-demo-stream:
	EFFECTUS_URL="http://localhost:8080" EFFECTUS_TOKEN="{{UI_FLOW_DEMO_TOKEN}}" {{UI_FLOW_DEMO_STREAM}}

# Open the flow demo UI in a browser (macOS/Linux).
ui-flow-demo-open:
	@if command -v open >/dev/null 2>&1; then open http://localhost:8080/ui; \
	elif command -v xdg-open >/dev/null 2>&1; then xdg-open http://localhost:8080/ui; \
	else echo "Open http://localhost:8080/ui"; fi

# Clean flow demo artifacts (stop the running process with Ctrl+C in its terminal).
ui-flow-demo-down:
	@echo "Stopping flow UI demo... (use Ctrl+C in the ui-flow-demo terminal if it's running)"
	@rm -rf out/flow_ui_demo

# Test migrations up and down
test-migrate:
	@echo "Testing migrations..."
	cd runtime && goose -dir {{MIGRATIONS_DIR}} postgres "{{DB_DSN}}" up
	cd runtime && goose -dir {{MIGRATIONS_DIR}} postgres "{{DB_DSN}}" reset
	cd runtime && goose -dir {{MIGRATIONS_DIR}} postgres "{{DB_DSN}}" up
	@echo "OK Migration tests complete"

# Complete development setup for SQL
dev-sql-setup: install-sql-tools setup-db migrate-up sql-generate
	@echo "OK SQL development environment ready!"

# Reset SQL development environment
dev-sql-reset: migrate-fresh sql-generate
	@echo "OK SQL development environment reset!"

# Validate all SQL and generated code
sql-validate: sql-generate-check
	@echo "Validating SQL queries..."
	cd runtime && sqlc vet
	@echo "OK Validation complete"

# Lint SQL files (requires sqlfluff)
sql-lint:
	@echo "Linting SQL files..."
	@if command -v sqlfluff >/dev/null 2>&1; then sqlfluff lint {{MIGRATIONS_DIR}}; else echo "WARN  sqlfluff not installed. Install with: pip install sqlfluff"; fi

# Format SQL files (requires sqlfluff)
sql-format:
	@echo "Formatting SQL files..."
	@if command -v sqlfluff >/dev/null 2>&1; then sqlfluff format {{MIGRATIONS_DIR}} --dialect postgres; else echo "WARN  sqlfluff not installed. Install with: pip install sqlfluff"; fi

# Generate schema documentation
schema-docs:
	@echo "Generating schema documentation..."
	@echo "Database Schema Documentation" > runtime/SCHEMA.md
	@echo "============================" >> runtime/SCHEMA.md
	@psql "{{DB_DSN}}" -c "\dt" >> runtime/SCHEMA.md

# === Warehouse Devstack (Trino + Iceberg + MinIO) ===

devstack-up:
	docker compose -f {{WAREHOUSE_DEVSTACK}}/docker-compose.yml up -d

devstack-down:
	docker compose -f {{WAREHOUSE_DEVSTACK}}/docker-compose.yml down

devstack-logs:
	docker compose -f {{WAREHOUSE_DEVSTACK}}/docker-compose.yml logs -f

devstack-seed-iceberg:
	{{WAREHOUSE_DEVSTACK}}/scripts/seed-iceberg.sh

devstack-seed-s3:
	{{WAREHOUSE_DEVSTACK}}/scripts/seed-s3.sh

devstack-seed-parquet:
	{{WAREHOUSE_DEVSTACK}}/scripts/seed-parquet.sh

devstack-trino-cli:
	{{WAREHOUSE_DEVSTACK}}/scripts/trino-cli.sh
	@echo "OK Schema documentation generated"

devstack-smoke-test:
	{{WAREHOUSE_DEVSTACK}}/scripts/smoke-test.sh

# === CDC Stack (Postgres + MySQL + RabbitMQ) ===

cdc-up:
	docker compose -f {{CDC_STACK}}/docker-compose.yml up -d

cdc-down:
	docker compose -f {{CDC_STACK}}/docker-compose.yml down

cdc-logs:
	docker compose -f {{CDC_STACK}}/docker-compose.yml logs -f

cdc-test:
	POSTGRES_DSN="postgres://effectus:effectus@localhost:5432/effectus_cdc?sslmode=disable" \
	MYSQL_HOST=127.0.0.1 \
	MYSQL_PORT=3306 \
	MYSQL_USER=effectus \
	MYSQL_PASSWORD=effectus \
	MYSQL_DATABASE=effectus_cdc \
	MYSQL_DSN="effectus:effectus@tcp(127.0.0.1:3306)/effectus_cdc?parseTime=true&multiStatements=true" \
	go test -tags=integration ./adapters/postgres ./adapters/mysql

# Clean generated SQL files
sql-clean:
	@echo "Cleaning generated SQL files..."
	rm -rf runtime/internal/db/*.go
	@echo "OK SQL clean complete"

# Clean everything including database (WARN DESTROYS ALL DATA)
sql-clean-all: sql-clean
	@echo "WARN  This will destroy database. Continue? (Press Enter to continue, Ctrl+C to cancel)"
	@read
	{{DOCKER_COMPOSE}} down -v postgres
	@echo "OK Complete SQL cleanup done"

# Open database shell
db-shell:
	@echo "Opening database shell..."
	psql "{{DB_DSN}}"

# Dump database schema and data
db-dump:
	@echo "Dumping database..."
	pg_dump "{{DB_DSN}}" > effectus_dump_$(date +%Y%m%d_%H%M%S).sql
	@echo "OK Database dumped"

# Restore database from dump
db-restore dump:
	@echo "WARN  This will overwrite the database. Continue? (Press Enter to continue, Ctrl+C to cancel)"
	@read
	@echo "Restoring database from {{dump}}..."
	psql "{{DB_DSN}}" < {{dump}}
	@echo "OK Database restored"

# === VS Code Extension Commands ===

# Install VS Code extension dependencies
vscode-install:
	@echo "Installing VS Code extension dependencies..."
	cd tools/vscode-extension && npm install
	@echo "OK VS Code extension dependencies installed"

# Compile TypeScript for VS Code extension  
vscode-compile:
	@echo "Compiling VS Code extension..."
	cd tools/vscode-extension && npm run compile
	@echo "OK VS Code extension compiled"

# Watch mode for VS Code extension development
vscode-watch:
	@echo "Starting VS Code extension watch mode..."
	cd tools/vscode-extension && npm run watch

# Package VS Code extension
vscode-package:
	@echo "Packaging VS Code extension..."
	cd tools/vscode-extension && npm run package
	@echo "OK VS Code extension packaged as .vsix file"

# Install packaged VS Code extension locally
vscode-install-local:
	@echo "Installing VS Code extension locally..."
	cd tools/vscode-extension && code --install-extension effectus-language-support-*.vsix
	@echo "OK VS Code extension installed locally"

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
	@echo "OK VS Code extension development environment ready!"
	@echo "Use 'just vscode-watch' for development"
	@echo "Use 'just vscode-package' to create .vsix file"

# === Schema Management ===

# Register a new verb schema
register-verb name input_schema output_schema:
	go run ./cmd/effectusc schema register-verb --name={{name}} --input="{{input_schema}}" --output="{{output_schema}}"

# Register a new fact schema
register-fact name schema:
	go run ./cmd/effectusc schema register-fact --name={{name}} --schema="{{schema}}"

# List all registered schemas
list-schemas:
	go run ./cmd/effectusc schema list

# Validate schema compatibility
validate-schemas:
	go run ./cmd/effectusc schema validate

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
	@echo "OK Complete development environment ready!"

# Watch for changes and rebuild (requires entr)
watch:
	find . -name "*.go" -o -name "*.proto" -o -name "*.sql" | entr -r just dev

# Start development server
serve:
	go run ./cmd/effectusd

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
	go doc -all ./... > docs/api.md

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
	GOOS=linux GOARCH=amd64 go build -o bin/effectusc-linux-amd64 ./cmd/effectusc
	GOOS=darwin GOARCH=amd64 go build -o bin/effectusc-darwin-amd64 ./cmd/effectusc
	GOOS=windows GOARCH=amd64 go build -o bin/effectusc-windows-amd64.exe ./cmd/effectusc
