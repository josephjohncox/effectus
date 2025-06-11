package main

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	_ "github.com/lib/pq" // PostgreSQL driver
	"github.com/pressly/goose/v3"
	"github.com/spf13/cobra"
)

// migrateCmd represents the migrate command group
var migrateCmd = &cobra.Command{
	Use:   "migrate",
	Short: "Database migration management",
	Long: `Manage database migrations using goose. This command group provides
operations for creating, running, and managing database schema migrations.

Examples:
  # Run all pending migrations
  effectusc migrate up

  # Rollback the last migration
  effectusc migrate down

  # Check migration status
  effectusc migrate status

  # Create a new migration
  effectusc migrate create add_new_column`,
}

var (
	migrateUpCmd      = createMigrateUpCmd()
	migrateDownCmd    = createMigrateDownCmd()
	migrateStatusCmd  = createMigrateStatusCmd()
	migrateCreateCmd  = createMigrateCreateCmd()
	migrateVersionCmd = createMigrateVersionCmd()
	migrateResetCmd   = createMigrateResetCmd()
	migrateRedoCmd    = createMigrateRedoCmd()
)

func init() {
	rootCmd.AddCommand(migrateCmd)

	// Add subcommands
	migrateCmd.AddCommand(migrateUpCmd)
	migrateCmd.AddCommand(migrateDownCmd)
	migrateCmd.AddCommand(migrateStatusCmd)
	migrateCmd.AddCommand(migrateCreateCmd)
	migrateCmd.AddCommand(migrateVersionCmd)
	migrateCmd.AddCommand(migrateResetCmd)
	migrateCmd.AddCommand(migrateRedoCmd)

	// Global flags for migrate commands
	migrateCmd.PersistentFlags().StringVar(&dbDSN, "dsn", "", "Database connection string")
	migrateCmd.PersistentFlags().StringVar(&migrationsDir, "migrations-dir", "effectus-go/runtime/migrations", "Path to migrations directory")
}

var (
	dbDSN         string
	migrationsDir string
)

// createMigrateUpCmd creates the migrate up command
func createMigrateUpCmd() *cobra.Command {
	var (
		byOne     bool
		toVersion int64
	)

	cmd := &cobra.Command{
		Use:   "up",
		Short: "Run pending migrations",
		Long: `Run all pending migrations or migrate to a specific version.

Examples:
  # Run all pending migrations
  effectusc migrate up

  # Run migrations one by one
  effectusc migrate up --by-one

  # Migrate to a specific version
  effectusc migrate up --to-version 20240115000001`,
		RunE: func(cmd *cobra.Command, args []string) error {
			db, err := openDB()
			if err != nil {
				return err
			}
			defer db.Close()

			if err := goose.SetDialect("postgres"); err != nil {
				return fmt.Errorf("failed to set dialect: %w", err)
			}

			if byOne {
				return goose.UpByOne(db, migrationsDir)
			}

			if toVersion > 0 {
				return goose.UpTo(db, migrationsDir, toVersion)
			}

			return goose.Up(db, migrationsDir)
		},
	}

	cmd.Flags().BoolVar(&byOne, "by-one", false, "Run migrations one by one")
	cmd.Flags().Int64Var(&toVersion, "to-version", 0, "Migrate to specific version")

	return cmd
}

// createMigrateDownCmd creates the migrate down command
func createMigrateDownCmd() *cobra.Command {
	var (
		byOne     bool
		toVersion int64
	)

	cmd := &cobra.Command{
		Use:   "down",
		Short: "Rollback migrations",
		Long: `Rollback the last migration or rollback to a specific version.

Examples:
  # Rollback the last migration
  effectusc migrate down

  # Rollback migrations one by one
  effectusc migrate down --by-one

  # Rollback to a specific version
  effectusc migrate down --to-version 20240115000001`,
		RunE: func(cmd *cobra.Command, args []string) error {
			db, err := openDB()
			if err != nil {
				return err
			}
			defer db.Close()

			if err := goose.SetDialect("postgres"); err != nil {
				return fmt.Errorf("failed to set dialect: %w", err)
			}

			if byOne {
				return goose.DownByOne(db, migrationsDir)
			}

			if toVersion > 0 {
				return goose.DownTo(db, migrationsDir, toVersion)
			}

			return goose.Down(db, migrationsDir)
		},
	}

	cmd.Flags().BoolVar(&byOne, "by-one", false, "Rollback migrations one by one")
	cmd.Flags().Int64Var(&toVersion, "to-version", 0, "Rollback to specific version")

	return cmd
}

// createMigrateStatusCmd creates the migrate status command
func createMigrateStatusCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show migration status",
		Long: `Show the status of all migrations including which ones have been applied.

Examples:
  # Show migration status
  effectusc migrate status`,
		RunE: func(cmd *cobra.Command, args []string) error {
			db, err := openDB()
			if err != nil {
				return err
			}
			defer db.Close()

			if err := goose.SetDialect("postgres"); err != nil {
				return fmt.Errorf("failed to set dialect: %w", err)
			}

			return goose.Status(db, migrationsDir)
		},
	}

	return cmd
}

// createMigrateCreateCmd creates the migrate create command
func createMigrateCreateCmd() *cobra.Command {
	var migrationtype string

	cmd := &cobra.Command{
		Use:   "create <name>",
		Short: "Create a new migration",
		Long: `Create a new migration file with the given name.

Examples:
  # Create a SQL migration
  effectusc migrate create add_user_table

  # Create a Go migration
  effectusc migrate create --type go seed_initial_data`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]

			if err := goose.SetDialect("postgres"); err != nil {
				return fmt.Errorf("failed to set dialect: %w", err)
			}

			// Ensure migrations directory exists
			if err := os.MkdirAll(migrationsDir, 0755); err != nil {
				return fmt.Errorf("failed to create migrations directory: %w", err)
			}

			// Create migration
			if migrationtype == "go" {
				return goose.CreateWithTemplate(nil, migrationsDir, goose.DefaultGoMigrationTemplate, name, migrationtype)
			}

			return goose.Create(nil, migrationsDir, name, migrationtype)
		},
	}

	cmd.Flags().StringVar(&migrationtype, "type", "sql", "Migration type (sql or go)")

	return cmd
}

// createMigrateVersionCmd creates the migrate version command
func createMigrateVersionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "version",
		Short: "Show current migration version",
		Long: `Show the current migration version of the database.

Examples:
  # Show current version
  effectusc migrate version`,
		RunE: func(cmd *cobra.Command, args []string) error {
			db, err := openDB()
			if err != nil {
				return err
			}
			defer db.Close()

			if err := goose.SetDialect("postgres"); err != nil {
				return fmt.Errorf("failed to set dialect: %w", err)
			}

			version, err := goose.GetDBVersion(db)
			if err != nil {
				return fmt.Errorf("failed to get version: %w", err)
			}

			fmt.Printf("Current version: %d\n", version)
			return nil
		},
	}

	return cmd
}

// createMigrateResetCmd creates the migrate reset command
func createMigrateResetCmd() *cobra.Command {
	var confirm bool

	cmd := &cobra.Command{
		Use:   "reset",
		Short: "Reset database by rolling back all migrations",
		Long: `Reset the database by rolling back all migrations. This will 
remove all tables and data created by migrations.

Examples:
  # Reset database (with confirmation)
  effectusc migrate reset --confirm`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !confirm {
				fmt.Print("This will reset the database and remove all data. Are you sure? (y/N): ")
				var response string
				fmt.Scanln(&response)
				if response != "y" && response != "Y" && response != "yes" {
					fmt.Println("Reset cancelled")
					return nil
				}
			}

			db, err := openDB()
			if err != nil {
				return err
			}
			defer db.Close()

			if err := goose.SetDialect("postgres"); err != nil {
				return fmt.Errorf("failed to set dialect: %w", err)
			}

			return goose.Reset(db, migrationsDir)
		},
	}

	cmd.Flags().BoolVar(&confirm, "confirm", false, "Skip confirmation prompt")

	return cmd
}

// createMigrateRedoCmd creates the migrate redo command
func createMigrateRedoCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "redo",
		Short: "Redo the last migration",
		Long: `Redo the last migration by rolling it back and applying it again.

Examples:
  # Redo the last migration
  effectusc migrate redo`,
		RunE: func(cmd *cobra.Command, args []string) error {
			db, err := openDB()
			if err != nil {
				return err
			}
			defer db.Close()

			if err := goose.SetDialect("postgres"); err != nil {
				return fmt.Errorf("failed to set dialect: %w", err)
			}

			return goose.Redo(db, migrationsDir)
		},
	}

	return cmd
}

// openDB opens a database connection for migrations
func openDB() (*sql.DB, error) {
	// Use DSN from flag or environment
	dsn := dbDSN
	if dsn == "" {
		dsn = os.Getenv("EFFECTUS_DB_DSN")
	}
	if dsn == "" {
		dsn = "postgres://effectus:effectus@localhost/effectus_dev?sslmode=disable"
	}

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Test connection
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return db, nil
}

// Migration helper commands

// initMigrateCmd adds initialization command
func init() {
	migrateInitCmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize migration system",
		Long: `Initialize the migration system by creating the migrations directory
and setting up the initial schema.

Examples:
  # Initialize migration system
  effectusc migrate init`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Create migrations directory
			if err := os.MkdirAll(migrationsDir, 0755); err != nil {
				return fmt.Errorf("failed to create migrations directory: %w", err)
			}

			// Create initial migration if it doesn't exist
			initialMigration := filepath.Join(migrationsDir, "001_initial_schema.sql")
			if _, err := os.Stat(initialMigration); os.IsNotExist(err) {
				fmt.Printf("Migrations directory created: %s\n", migrationsDir)
				fmt.Printf("Initial migration already exists: %s\n", initialMigration)
			} else {
				fmt.Printf("Migration system initialized in: %s\n", migrationsDir)
			}

			return nil
		},
	}

	migrateCmd.AddCommand(migrateInitCmd)

	// Add development helper commands
	migrateDevCmd := &cobra.Command{
		Use:   "dev",
		Short: "Development migration helpers",
		Long:  "Development helpers for working with migrations",
	}

	// Fresh command - reset and run all migrations
	migrateFreshCmd := &cobra.Command{
		Use:   "fresh",
		Short: "Reset and run all migrations (development only)",
		Long: `Reset the database and run all migrations from scratch.
This is useful during development but should NEVER be used in production.

Examples:
  # Fresh migration (development only)
  effectusc migrate dev fresh --confirm`,
		RunE: func(cmd *cobra.Command, args []string) error {
			var confirm bool
			cmd.Flags().BoolVar(&confirm, "confirm", false, "Skip confirmation")

			if !confirm {
				fmt.Print("This will destroy all data and recreate the database. Continue? (y/N): ")
				var response string
				fmt.Scanln(&response)
				if response != "y" && response != "Y" {
					return fmt.Errorf("operation cancelled")
				}
			}

			db, err := openDB()
			if err != nil {
				return err
			}
			defer db.Close()

			if err := goose.SetDialect("postgres"); err != nil {
				return err
			}

			// Reset and migrate
			if err := goose.Reset(db, migrationsDir); err != nil {
				return fmt.Errorf("failed to reset: %w", err)
			}

			if err := goose.Up(db, migrationsDir); err != nil {
				return fmt.Errorf("failed to migrate up: %w", err)
			}

			fmt.Println("✅ Database reset and migrations applied successfully")
			return nil
		},
	}

	// Seed command - run seed migrations
	migrateSeedCmd := &cobra.Command{
		Use:   "seed",
		Short: "Run seed data migrations",
		Long: `Run migrations that contain seed data for development.

Examples:
  # Run seed migrations
  effectusc migrate dev seed`,
		RunE: func(cmd *cobra.Command, args []string) error {
			db, err := openDB()
			if err != nil {
				return err
			}
			defer db.Close()

			if err := goose.SetDialect("postgres"); err != nil {
				return err
			}

			// This would run seed-specific migrations
			// For now, just run all migrations
			return goose.Up(db, migrationsDir)
		},
	}

	migrateDevCmd.AddCommand(migrateFreshCmd)
	migrateDevCmd.AddCommand(migrateSeedCmd)
	migrateCmd.AddCommand(migrateDevCmd)

	// Quick command - common migration tasks
	migrateQuickCmd := &cobra.Command{
		Use:   "quick",
		Short: "Quick migration shortcuts",
		Long:  "Quick shortcuts for common migration tasks",
	}

	// Rollback N migrations
	migrateRollbackCmd := &cobra.Command{
		Use:   "rollback [N]",
		Short: "Rollback N migrations (default: 1)",
		Long: `Rollback the specified number of migrations.

Examples:
  # Rollback 1 migration
  effectusc migrate quick rollback

  # Rollback 3 migrations
  effectusc migrate quick rollback 3`,
		RunE: func(cmd *cobra.Command, args []string) error {
			n := 1
			if len(args) > 0 {
				var err error
				n, err = strconv.Atoi(args[0])
				if err != nil {
					return fmt.Errorf("invalid number: %s", args[0])
				}
			}

			db, err := openDB()
			if err != nil {
				return err
			}
			defer db.Close()

			if err := goose.SetDialect("postgres"); err != nil {
				return err
			}

			for i := 0; i < n; i++ {
				if err := goose.Down(db, migrationsDir); err != nil {
					return fmt.Errorf("failed to rollback migration %d: %w", i+1, err)
				}
			}

			fmt.Printf("✅ Rolled back %d migration(s)\n", n)
			return nil
		},
	}

	migrateQuickCmd.AddCommand(migrateRollbackCmd)
	migrateCmd.AddCommand(migrateQuickCmd)
}
