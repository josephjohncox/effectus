# Effectus Development Commands

# Variables
DB_DSN := env_var_or_default("DB_DSN", "postgres://effectus:effectus@localhost:55433/effectus_saga?sslmode=disable")
MIGRATIONS_DIR := "migrations"
DOCKER_COMPOSE := "docker compose -f examples/saga_stack/docker-compose.yml"
UI_SMOKE_COMPOSE := "COMPOSE_PROJECT_NAME=effectus-ui-demo-smoke docker compose -f examples/saga_stack/docker-compose.yml"
WAREHOUSE_DEVSTACK := "examples/warehouse_sources/devstack"
CDC_STACK := "examples/cdc_stack"
SAGA_STACK := "examples/saga_stack"
UI_DEMO_RULES := "examples/fraud_e2e/rules"
UI_DEMO_SCHEMA := "examples/fraud_e2e/schema"
UI_DEMO_VERBS := "examples/fraud_e2e/schema/fraud_verbs.json"
UI_DEMO_VERB_DIR := "examples/fraud_e2e/verbs"
UI_DEMO_EXTENSIONS := "examples/fraud_e2e/extensions"
UI_DEMO_BUNDLE := "out/ui_demo/bundle.json"
UI_DEMO_FACTS := "examples/fraud_e2e/data/facts_payload.json"
UI_DEMO_TOKEN := "demo-token"
UI_FLOW_DEMO_RULES := "examples/flow_ui_demo/rules"
UI_FLOW_DEMO_SCHEMA := "examples/flow_ui_demo/schema"
UI_FLOW_DEMO_VERBS := "examples/flow_ui_demo/schema/flow_verbs.json"
UI_FLOW_DEMO_VERB_DIR := "examples/flow_ui_demo/verbs"
UI_FLOW_DEMO_EXTENSIONS := "examples/flow_ui_demo/extensions"
UI_FLOW_DEMO_BUNDLE := "out/flow_ui_demo/bundle.json"
UI_FLOW_DEMO_FACTS := "examples/flow_ui_demo/data/facts_payload.json"
UI_FLOW_DEMO_STREAM := "examples/flow_ui_demo/scripts/stream_facts.sh"
UI_FLOW_DEMO_TOKEN := "flow-demo-token"
UI_FLOW_SQL_STACK := "examples/flow_ui_demo/sql_scrape"
UI_FLOW_SQL_DSN := "postgres://effectus:effectus@localhost:55432/effectus_ui_demo?sslmode=disable"
DAEMON_POSTGRES_DSN := "postgres://effectus:effectus@localhost:55433/effectus_saga?sslmode=disable"
SAGA_REDIS_ADDR := "localhost:56379"

# Default recipe
default:
	@just --list

# Install development dependencies
install:
	go mod download
	go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.36.11
	go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.6.0
	go install github.com/bufbuild/buf/cmd/buf@v1.50.0
	just check-protobuf-tools

# Verify the generation toolchain matches CI.
check-protobuf-tools:
	@protoc-gen-go --version | grep -F 'v1.36.11'
	@protoc-gen-go-grpc --version | grep -F '1.6.0'
	@buf --version | grep -Fx '1.50.0'

# Install SQL tooling (sqlc and goose)
install-sql-tools:
	@echo "Installing SQL tooling..."
	go install github.com/sqlc-dev/sqlc/cmd/sqlc@v1.29.0
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

# Model-check saga recovery and runtime generation publication
formal-check:
	@command -v tlc >/dev/null 2>&1 || { echo "ERROR tlc is required"; exit 1; }
	@set -eu; saga_dir=$(mktemp -d); generation_dir=$(mktemp -d); trap 'rm -rf "$saga_dir" "$generation_dir"' EXIT; tlc -metadir "$saga_dir" formal/Saga.tla -config formal/Saga.cfg; tlc -metadir "$generation_dir" formal/GenerationSwap.tla -config formal/GenerationSwap.cfg

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
	scripts/check-buf-breaking.sh '.git#branch=main'

# Push to buf registry (requires authentication)
buf-push:
	buf push

# === SQL Database Commands ===

# Setup development database with Docker
setup-db:
	@echo "Starting saga-stack PostgreSQL with Docker..."
	{{DOCKER_COMPOSE}} up -d postgres
	@echo "Waiting for PostgreSQL on port 55433..."
	@for attempt in $(seq 1 60); do \
		{{DOCKER_COMPOSE}} exec -T postgres pg_isready -U effectus -d effectus_saga >/dev/null 2>&1 && exit 0; \
		sleep 1; \
	done; echo "ERROR PostgreSQL did not become ready"; exit 1
	@echo "OK Database ready: {{DAEMON_POSTGRES_DSN}}"

# Run an isolated PostgreSQL restore drill and write evidence under out/restore-drill.
restore-drill: setup-db
	EFFECTUS_POSTGRES_DSN='{{DAEMON_POSTGRES_DSN}}' ./scripts/restore-drill.sh

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

# Run all service-specific integration tests. Required variables must be set.
test-integration: test-integration-postgres test-integration-redis test-integration-cdc test-integration-kafka test-integration-s3

# Run PostgreSQL integration tests.
test-integration-postgres:
	@test -n "${DB_DSN:-}" || { echo "ERROR DB_DSN is required"; exit 1; }
	EFFECTUS_POSTGRES_DSN="$DB_DSN" go run ./cmd/effectusd --database-migrations=apply
	DB_DSN="$DB_DSN" POSTGRES_DSN="${POSTGRES_DSN:-$DB_DSN}" go test -p 1 -v -tags=integration ./runtime/... ./schema ./cmd/effectusd

# Run Redis integration tests.
test-integration-redis:
	@test -n "${REDIS_ADDR:-}" || { echo "ERROR REDIS_ADDR is required"; exit 1; }
	REDIS_ADDR="$REDIS_ADDR" go test -v -tags=integration ./schema ./adapters/redis

# Run Kafka integration tests.
test-integration-kafka:
	@test -n "${KAFKA_BROKERS:-}" || { echo "ERROR KAFKA_BROKERS is required"; exit 1; }
	KAFKA_BROKERS="$KAFKA_BROKERS" go test -v -tags=integration ./adapters/kafka -run '^TestKafkaConsumerGroupCommitAndRestart$' -count=1

# Run S3 adapter integration tests.
test-integration-s3:
	@test -n "${S3_ENDPOINT:-}" || { echo "ERROR S3_ENDPOINT is required"; exit 1; }
	@test -n "${S3_BUCKET:-}" || { echo "ERROR S3_BUCKET is required"; exit 1; }
	S3_ENDPOINT="$S3_ENDPOINT" S3_BUCKET="$S3_BUCKET" S3_REGION="${S3_REGION:-us-east-1}" \
		S3_ACCESS_KEY="${S3_ACCESS_KEY:-}" S3_SECRET_KEY="${S3_SECRET_KEY:-}" \
		go test -v -tags=integration ./adapters/s3

# Execute published snippets and command/reference contracts.
test-docs:
	@set -eu; tmp=$(mktemp -d); trap 'rm -rf "$tmp"' EXIT; \
	cp examples/fraud_e2e/rules/fraud_rules.eff "$tmp/rule.eff"; \
	go run ./cmd/effectusc parse --verbose "$tmp/rule.eff" >/dev/null; \
	go run ./cmd/effectusc format --stdout "$tmp/rule.eff" >/dev/null; \
	go run ./cmd/effectusc bundle --name flow-ui-demo --version 1.0.0 \
		--schema-dir examples/flow_ui_demo/schema --verb-dir examples/flow_ui_demo/verbs \
		--verbschema examples/flow_ui_demo/schema/flow_verbs.json --rules-dir examples/flow_ui_demo/rules \
		--output "$tmp/bundle.json"; test -s "$tmp/bundle.json"; \
	go test ./cmd/effectusc -run 'TestDocumentedCompilerCommands|TestFormatCheckDoesNotWrite'; \
	go test ./cmd/effectusd -run TestDocumentedDaemonFlags; \
	(cd examples && go test ./grpc_execution); \
	buf generate --path effectus --path runtime --template buf.gen.python.yaml --output "$tmp/python"; \
	python3 -m venv "$tmp/venv"; "$tmp/venv/bin/pip" --quiet install protobuf==5.29.3; \
	PYTHONPATH="$tmp/python" "$tmp/venv/bin/python" docs/tests/python_typed_facts.py; \
	! grep -R -n -F 'token: "write-token"' docs/RUNTIME_CONFIG.md charts/effectusd/README.md; \
	! grep -R -n -E 'delivery_ledger:|poison_audit:|--pprof-addr|--saga-postgres-dsn' docs README.md examples/README.md

# Run every Go example module. Keep this list synchronized with CI.
test-examples:
	@set -eu; for module in \
		examples \
		examples/buf_integration \
		examples/business_facts \
		examples/business_verbs \
		examples/coherent_flow \
		examples/extension_system \
		examples/fraud_e2e/mocks; do \
		echo "==> $module"; (cd "$module" && go test ./...); \
	done; \
	(cd examples/coherent_flow && go run .); \
	(cd examples && go run ./fraud_e2e); \
	(cd examples && go run ./multi_bundle_runtime)

# Render and validate all supported Helm deployment fixtures.
test-helm:
	@set -eu; command -v helm >/dev/null; command -v kubeconform >/dev/null; \
	helm lint charts/effectusd -f charts/effectusd/ci/oci-values.yaml; \
	for fixture in oci config grpc-tls persistence; do \
		values=charts/effectusd/ci/$fixture-values.yaml; \
		helm template effectusd charts/effectusd -f "$values" > "/tmp/effectusd-$fixture.yaml"; \
		kubeconform -strict -summary "/tmp/effectusd-$fixture.yaml"; \
	done; \
	grep -F -- '--oci-cache-dir=/data/bundles' /tmp/effectusd-config.yaml

# KAFKA_BROKERS must name a real broker; this recipe must never silently skip.
test-kafka-integration:
	@set -eu; test -n "${KAFKA_BROKERS:-}" || { echo "ERROR KAFKA_BROKERS is required"; exit 1; }; \
	KAFKA_BROKERS="$KAFKA_BROKERS" go test -v -count=1 -tags=integration ./adapters/kafka -run '^TestKafkaConsumerGroupCommitAndRestart$'

# Run CDC adapter integration suites against explicit service endpoints.
test-integration-cdc:
	@test -n "$${POSTGRES_DSN:-}" || { echo "ERROR POSTGRES_DSN is required"; exit 1; }
	@test -n "$${MYSQL_DSN:-}" || { echo "ERROR MYSQL_DSN is required"; exit 1; }
	POSTGRES_DSN="$${POSTGRES_DSN}" MYSQL_DSN="$${MYSQL_DSN}" \
		go test -v -tags=integration ./adapters/postgres ./adapters/mysql


# === UI Demo ===

# Build a demo bundle (fraud rules) and start the status UI/runtime.
ui-demo: setup-db
	@mkdir -p out/ui_demo
	go run ./cmd/effectusc bundle \
		--name fraud-ui-demo \
		--version 1.0.0 \
		--schema-dir {{UI_DEMO_SCHEMA}} \
		--verb-dir {{UI_DEMO_VERB_DIR}} \
		--verbschema {{UI_DEMO_VERBS}} \
		--rules-dir {{UI_DEMO_RULES}} \
		--output {{UI_DEMO_BUNDLE}}
	EFFECTUS_POSTGRES_DSN='{{DAEMON_POSTGRES_DSN}}' go run ./cmd/effectusd --database-migrations=apply
	@echo "Starting effectusd UI..."
	@echo "Token: {{UI_DEMO_TOKEN}}"
	@echo "Open http://localhost:8080/ui"
	@echo ""
	@echo "Example ingest (new facts):"
	@echo "curl --fail-with-body -X POST http://localhost:8080/api/facts \\"
	@echo "  -H \"Authorization: Bearer {{UI_DEMO_TOKEN}}\" \\"
	@echo "  -H \"Idempotency-Key: fraud-demo-seed-v1\" \\"
	@echo "  -H \"Content-Type: application/json\" \\"
	@echo "  -d @{{UI_DEMO_FACTS}}"
	@echo ""
	@echo "Example dry run (use stored facts):"
	@echo "curl -X POST http://localhost:8080/api/playground/dry-run \\"
	@echo "  -H \"Authorization: Bearer {{UI_DEMO_TOKEN}}\" \\"
	@echo "  -H \"Content-Type: application/json\" \\"
	@echo "  -d '{\"universe\":\"default\",\"mode\":\"both\",\"use_stored\":true}'"
	EFFECTUS_API_TOKEN={{UI_DEMO_TOKEN}} EFFECTUS_POSTGRES_DSN='{{DAEMON_POSTGRES_DSN}}' go run ./cmd/effectusd \
		--bundle {{UI_DEMO_BUNDLE}} \
		--http-addr :8080 \
		--extensions-dir {{UI_DEMO_EXTENSIONS}} \
		--facts-store file \
		--facts-path out/ui_demo/facts.json

# Seed the demo facts into the running UI instance.
ui-demo-seed:
	curl --fail-with-body -X POST http://localhost:8080/api/facts \
		-H "Authorization: Bearer {{UI_DEMO_TOKEN}}" \
		-H "Idempotency-Key: fraud-demo-seed-v1" \
		-H "Content-Type: application/json" \
		-d @{{UI_DEMO_FACTS}}

# Cold-start PostgreSQL and the checked UI path, then prove durable HTTP replay.
ui-demo-smoke:
	@set -eu; pid=''; \
	trap 'test -z "$pid" || { kill "$pid" >/dev/null 2>&1 || true; wait "$pid" >/dev/null 2>&1 || true; }; {{UI_SMOKE_COMPOSE}} down -v >/dev/null 2>&1 || true' EXIT INT TERM; \
	{{UI_SMOKE_COMPOSE}} down -v >/dev/null 2>&1 || true; COMPOSE_PROJECT_NAME=effectus-ui-demo-smoke just setup-db; \
	mkdir -p out/ui_demo; \
	go run ./cmd/effectusc bundle --name fraud-ui-demo --version 1.0.0 \
		--schema-dir {{UI_DEMO_SCHEMA}} --verb-dir {{UI_DEMO_VERB_DIR}} \
		--verbschema {{UI_DEMO_VERBS}} --rules-dir {{UI_DEMO_RULES}} --output {{UI_DEMO_BUNDLE}}; \
	go build -o out/ui_demo/effectusd ./cmd/effectusd; \
	EFFECTUS_POSTGRES_DSN='{{DAEMON_POSTGRES_DSN}}' out/ui_demo/effectusd --database-migrations=apply; \
	EFFECTUS_API_TOKEN={{UI_DEMO_TOKEN}} EFFECTUS_POSTGRES_DSN='{{DAEMON_POSTGRES_DSN}}' \
		out/ui_demo/effectusd --bundle {{UI_DEMO_BUNDLE}} --http-addr 127.0.0.1:18080 \
		--metrics-addr '' --extensions-dir {{UI_DEMO_EXTENSIONS}} --facts-store memory \
		>out/ui_demo/effectusd.log 2>&1 & pid=$!; \
	for attempt in $(seq 1 90); do curl --fail --silent http://127.0.0.1:18080/readyz >/dev/null && break; \
		kill -0 $pid 2>/dev/null || { cat out/ui_demo/effectusd.log; exit 1; }; sleep 1; done; \
	curl --fail --silent http://127.0.0.1:18080/readyz >/dev/null; \
	! grep -q 'verb hash mismatch' out/ui_demo/effectusd.log; \
	first=$(curl --fail-with-body --silent -X POST http://127.0.0.1:18080/api/facts \
		-H 'Authorization: Bearer {{UI_DEMO_TOKEN}}' -H 'Idempotency-Key: ui-smoke-v1' \
		-H 'Content-Type: application/json' -d @{{UI_DEMO_FACTS}}); \
	second=$(curl --fail-with-body --silent -X POST http://127.0.0.1:18080/api/facts \
		-H 'Authorization: Bearer {{UI_DEMO_TOKEN}}' -H 'Idempotency-Key: ui-smoke-v1' \
		-H 'Content-Type: application/json' -d @{{UI_DEMO_FACTS}}); \
	FIRST="$first" SECOND="$second" python3 -c 'import json,os; a=json.loads(os.environ["FIRST"]); b=json.loads(os.environ["SECOND"]); assert a["execution_id"] == b["execution_id"]'; \
	status=$(curl --silent --output out/ui_demo/conflict.json --write-out '%{http_code}' -X POST http://127.0.0.1:18080/api/facts \
		-H 'Authorization: Bearer {{UI_DEMO_TOKEN}}' -H 'Idempotency-Key: ui-smoke-v1' \
		-H 'Content-Type: application/json' -d '{"universe":"demo","facts":{"transaction":{"id":"changed"}}}'); \
	test "$status" = 409; echo "OK checked UI cold-start and replay smoke passed"

# Start a complete local generated-gRPC execution path.
grpc-execution-smoke:
	@set -eu; pid=''; \
	trap 'test -z "$pid" || { kill "$pid" >/dev/null 2>&1 || true; wait "$pid" >/dev/null 2>&1 || true; }; {{UI_SMOKE_COMPOSE}} down -v >/dev/null 2>&1 || true' EXIT INT TERM; \
	{{UI_SMOKE_COMPOSE}} down -v >/dev/null 2>&1 || true; COMPOSE_PROJECT_NAME=effectus-ui-demo-smoke just setup-db; \
	mkdir -p out/grpc_execution; \
	go run ./cmd/effectusc bundle --name flow-ui-demo --version 1.0.0 \
		--schema-dir {{UI_FLOW_DEMO_SCHEMA}} --verb-dir {{UI_FLOW_DEMO_VERB_DIR}} \
		--verbschema {{UI_FLOW_DEMO_VERBS}} --rules-dir {{UI_FLOW_DEMO_RULES}} --output out/grpc_execution/bundle.json; \
	go build -o out/grpc_execution/effectusd ./cmd/effectusd; \
	EFFECTUS_POSTGRES_DSN='{{DAEMON_POSTGRES_DSN}}' out/grpc_execution/effectusd --database-migrations=apply; \
	EFFECTUS_API_TOKEN={{UI_FLOW_DEMO_TOKEN}} EFFECTUS_POSTGRES_DSN='{{DAEMON_POSTGRES_DSN}}' \
		out/grpc_execution/effectusd --bundle out/grpc_execution/bundle.json --http-addr 127.0.0.1:18082 \
		--grpc-addr 127.0.0.1:18081 --grpc-allow-insecure --metrics-addr '' \
		--extensions-dir {{UI_FLOW_DEMO_EXTENSIONS}} --facts-store memory \
		>out/grpc_execution/effectusd.log 2>&1 & pid=$!; \
	for attempt in $(seq 1 90); do curl --fail --silent http://127.0.0.1:18082/readyz >/dev/null && break; \
		kill -0 $pid 2>/dev/null || { cat out/grpc_execution/effectusd.log; exit 1; }; sleep 1; done; \
	curl --fail --silent http://127.0.0.1:18082/readyz >/dev/null; \
	(cd examples && go run ./grpc_execution --address 127.0.0.1:18081 --token {{UI_FLOW_DEMO_TOKEN}} --ruleset flow-ui-demo --version 1.0.0); \
	echo "OK generated gRPC execution smoke passed"

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
ui-flow-demo: setup-db
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
	@echo "curl --fail-with-body -X POST http://localhost:8080/api/facts \\"
	@echo "  -H \"Authorization: Bearer {{UI_FLOW_DEMO_TOKEN}}\" \\"
	@echo "  -H \"Idempotency-Key: flow-demo-seed-v1\" \\"
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
	@echo ""
	@echo "SQL scrape mock (Postgres):"
	@echo "just ui-flow-demo-sql-up"
	@echo "just ui-flow-demo-sql-scrape"
	@echo "just ui-flow-demo-sql-bump  # insert a new row"
	EFFECTUS_API_TOKEN={{UI_FLOW_DEMO_TOKEN}} EFFECTUS_POSTGRES_DSN='{{DAEMON_POSTGRES_DSN}}' go run ./cmd/effectusd \
		--bundle {{UI_FLOW_DEMO_BUNDLE}} \
		--http-addr :8080 \
		--extensions-dir {{UI_FLOW_DEMO_EXTENSIONS}} \
		--facts-store file \
		--facts-path out/flow_ui_demo/facts.json

# Cold-start and execute the documented flow UI ingest and stream journey.
ui-flow-demo-smoke:
	@set -eu; pid=''; \
	trap 'test -z "$pid" || { kill "$pid" >/dev/null 2>&1 || true; wait "$pid" >/dev/null 2>&1 || true; }; {{UI_SMOKE_COMPOSE}} down -v >/dev/null 2>&1 || true' EXIT INT TERM; \
	{{UI_SMOKE_COMPOSE}} down -v >/dev/null 2>&1 || true; COMPOSE_PROJECT_NAME=effectus-ui-demo-smoke just setup-db; \
	mkdir -p out/flow_ui_demo; \
	go run ./cmd/effectusc bundle --name flow-ui-demo --version 1.0.0 \
		--schema-dir {{UI_FLOW_DEMO_SCHEMA}} --verb-dir {{UI_FLOW_DEMO_VERB_DIR}} \
		--verbschema {{UI_FLOW_DEMO_VERBS}} --rules-dir {{UI_FLOW_DEMO_RULES}} --output {{UI_FLOW_DEMO_BUNDLE}}; \
	go build -o out/flow_ui_demo/effectusd ./cmd/effectusd; \
	EFFECTUS_POSTGRES_DSN='{{DAEMON_POSTGRES_DSN}}' out/flow_ui_demo/effectusd --database-migrations=apply; \
	EFFECTUS_API_TOKEN={{UI_FLOW_DEMO_TOKEN}} EFFECTUS_POSTGRES_DSN='{{DAEMON_POSTGRES_DSN}}' \
		out/flow_ui_demo/effectusd --bundle {{UI_FLOW_DEMO_BUNDLE}} --http-addr 127.0.0.1:18084 \
		--metrics-addr '' --extensions-dir {{UI_FLOW_DEMO_EXTENSIONS}} --facts-store memory \
		>out/flow_ui_demo/effectusd.log 2>&1 & pid=$!; \
	for attempt in $(seq 1 90); do curl --fail --silent http://127.0.0.1:18084/readyz >/dev/null && break; \
		kill -0 $pid 2>/dev/null || { cat out/flow_ui_demo/effectusd.log; exit 1; }; sleep 1; done; \
	curl --fail --silent http://127.0.0.1:18084/readyz >/dev/null; \
	curl --fail-with-body --silent -X POST http://127.0.0.1:18084/api/facts \
		-H 'Authorization: Bearer {{UI_FLOW_DEMO_TOKEN}}' -H 'Idempotency-Key: flow-demo-seed-v1' \
		-H 'Content-Type: application/json' -d @{{UI_FLOW_DEMO_FACTS}} >/dev/null; \
	EFFECTUS_URL=http://127.0.0.1:18084 EFFECTUS_TOKEN={{UI_FLOW_DEMO_TOKEN}} {{UI_FLOW_DEMO_STREAM}}; \
	echo "OK flow UI readiness, seed, and stream smoke passed"

# Seed the flow demo facts into the running UI instance.
ui-flow-demo-seed:
	curl --fail-with-body -X POST http://localhost:8080/api/facts \
		-H "Authorization: Bearer {{UI_FLOW_DEMO_TOKEN}}" \
		-H "Idempotency-Key: flow-demo-seed-v1" \
		-H "Content-Type: application/json" \
		-d @{{UI_FLOW_DEMO_FACTS}}

# Stream fact updates (simulated streaming sources).
ui-flow-demo-stream:
	EFFECTUS_URL="http://localhost:8080" EFFECTUS_TOKEN="{{UI_FLOW_DEMO_TOKEN}}" {{UI_FLOW_DEMO_STREAM}}

# Start the SQL scrape mock (Postgres).
ui-flow-demo-sql-up:
	docker compose -f {{UI_FLOW_SQL_STACK}}/docker-compose.yml up -d

# Run the SQL scrape poller and forward facts into the UI demo.
ui-flow-demo-sql-scrape:
	EFFECTUS_URL="http://localhost:8080" EFFECTUS_TOKEN="{{UI_FLOW_DEMO_TOKEN}}" SQL_SCRAPE_DSN="{{UI_FLOW_SQL_DSN}}" go run ./examples/flow_ui_demo/sql_scrape

# Insert an update row in the SQL scrape mock.
ui-flow-demo-sql-bump:
	docker compose -f {{UI_FLOW_SQL_STACK}}/docker-compose.yml exec -T postgres psql -U effectus -d effectus_ui_demo -f /seed/insert_update.sql

# Stop the SQL scrape mock.
ui-flow-demo-sql-down:
	docker compose -f {{UI_FLOW_SQL_STACK}}/docker-compose.yml down -v

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

# Lint style-clean durable migrations. Older migrations remain a frozen baseline.
sql-lint:
	@echo "Linting durable runtime migrations..."
	@command -v sqlfluff >/dev/null 2>&1 || { echo "ERROR sqlfluff is required. Install sqlfluff 3.5.0"; exit 1; }
	sqlfluff lint --dialect postgres \
		schema/migrations/10002_execution_ledger.sql \
		schema/migrations/10003_kafka_delivery_ledger.sql \
		schema/migrations/10004_retention_indexes.sql

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

# === Saga Stack (Postgres + Redis) ===

saga-up:
	docker compose -f {{SAGA_STACK}}/docker-compose.yml up -d

saga-down:
	docker compose -f {{SAGA_STACK}}/docker-compose.yml down

saga-logs:
	docker compose -f {{SAGA_STACK}}/docker-compose.yml logs -f

saga-test:
	POSTGRES_DSN="{{DAEMON_POSTGRES_DSN}}" REDIS_ADDR="{{SAGA_REDIS_ADDR}}" go test -v -tags=integration ./cmd/effectusd

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

# Run the complete generated gRPC execution example.
example-grpc-execution:
	just grpc-execution-smoke

# Run checked rules inside a Go application.
example-embedded-orders:
	cd examples && go run ./embedded_orders

# Run effectusd with a separate PostgreSQL-backed business executor.
example-standalone-executor:
	examples/standalone_executor/scripts/run.sh

# Stop and remove the standalone executor stack.
example-standalone-executor-down:
	examples/standalone_executor/scripts/down.sh

# Run the modern SQL usage example
example-modern-sql:
	cd examples/modern_sql_usage && go run main.go

# === Documentation ===

# Build the GitHub Pages documentation after installing requirements-docs.txt.
docs:
	NO_MKDOCS_2_WARNING=true mkdocs build --strict

# Serve the GitHub Pages documentation locally.
docs-serve:
	NO_MKDOCS_2_WARNING=true mkdocs serve

# === Release ===

# Prepare release (bump version, tag, push)
release version:
	git tag v{{version}}
	git push origin v{{version}}
	gh release view v{{version}} >/dev/null 2>&1 || gh release create v{{version}} --verify-tag --generate-notes
	just buf-push

# Create release binaries
release-build:
	#!/usr/bin/env sh
	set -eu
	mkdir -p bin
	for target in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64; do
		os="${target%/*}"
		arch="${target#*/}"
		suffix=""
		if [ "$os" = windows ]; then suffix=".exe"; fi
		CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" go build -trimpath -o "bin/effectusc-$os-$arch$suffix" ./cmd/effectusc
		CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" go build -trimpath -o "bin/effectusd-$os-$arch$suffix" ./cmd/effectusd
	done

# Create GitHub release (requires gh CLI)
release-gh version:
	gh release create v{{version}} --generate-notes
