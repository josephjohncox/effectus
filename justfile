# Effectus Development Commands

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

# Initialize buf workspace (run once)
buf-init:
	buf mod init

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

# Create a new protobuf module
buf-mod-init name:
	mkdir -p proto/{{name}}
	cd proto/{{name}} && buf mod init

# Validate all protobuf schemas
buf-validate:
	buf lint
	buf build
	-buf breaking --against '.git#branch=main'

# Update buf dependencies
buf-update:
	buf mod update

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
	echo "Generated clients for Go, Python, TypeScript, Java, and Rust"

# === Development Workflow ===

# Complete development workflow: format, lint, test, build
dev:
	just fmt
	just lint
	just test
	just build

# Watch for changes and rebuild (requires entr)
watch:
	find . -name "*.go" -o -name "*.proto" | entr -r just dev

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

# === Documentation ===

# Generate documentation
docs:
	go doc -all ./effectus-go/... > docs/api.md
	buf generate --template buf.gen.docs.yaml

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