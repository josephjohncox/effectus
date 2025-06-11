# Add these dependencies to your go.mod for modern SQL tooling

require (
	github.com/google/uuid v1.5.0
	github.com/jackc/pgx/v5 v5.5.1
	github.com/jackc/pgx/v5/pgxpool v5.5.1
	github.com/lib/pq v1.10.9
	github.com/pressly/goose/v3 v3.17.0
)

# Development tools (install separately)
# go install github.com/sqlc-dev/sqlc/cmd/sqlc@v1.25.0
# go install github.com/pressly/goose/v3/cmd/goose@v3.17.0

# Optional: SQL linting and formatting
# pip install sqlfluff  # For SQL linting and formatting 