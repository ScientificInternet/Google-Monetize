#!/bin/bash

# Database migration script for billing service
# Uses golang-migrate to manage database schema changes

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Configuration
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
MIGRATIONS_DIR="$SCRIPT_DIR/../migrations"
CONFIG_FILE="$MIGRATIONS_DIR/config.yaml"

# Helper functions
log_info() {
    echo -e "${GREEN}[INFO]${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# Check if DATABASE_URL is set
check_database_url() {
    if [ -z "$DATABASE_URL" ]; then
        log_error "DATABASE_URL environment variable is not set"
        log_info "Please set DATABASE_URL or run with: export DATABASE_URL=your_database_url"
        exit 1
    fi
    log_info "Using database URL: ${DATABASE_URL:0:20}..."
}

# Get database URL from environment or Secret Manager
get_database_url() {
    local db_url="$DATABASE_URL"

    # Try to get from Secret Manager if not set directly
    if [ -z "$db_url" ] && [ -n "$DATABASE_URL_SECRET_NAME" ]; then
        log_info "Attempting to get database URL from Secret Manager..."
        db_url=$(gcloud secrets versions access latest --secret="$DATABASE_URL_SECRET_NAME" 2>/dev/null || echo "")
    fi

    if [ -z "$db_url" ]; then
        log_error "Could not retrieve database URL from environment or Secret Manager"
        exit 1
    fi

    export DATABASE_URL="$db_url"
}

# Initialize migrations directory
init_migrations() {
    log_info "Initializing migrations directory..."
    mkdir -p "$MIGRATIONS_DIR"

    # Create first migration if it doesn't exist
    if [ ! -f "$MIGRATIONS_DIR/000001_initial_schema.up.sql" ]; then
        log_info "Creating initial migration template..."
        cat > "$MIGRATIONS_DIR/000001_initial_schema.up.sql" << 'EOF'
-- Initial schema for billing service
-- This migration sets up the basic database structure

-- Create billing schema if it doesn't exist
CREATE SCHEMA IF NOT EXISTS billing;

-- Create users table for billing information
CREATE TABLE IF NOT EXISTS billing.users (
    user_id UUID PRIMARY KEY,
    email VARCHAR(255) UNIQUE NOT NULL,
    balance BIGINT DEFAULT 0,
    subscription_tier VARCHAR(50) DEFAULT 'starter',
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    last_login TIMESTAMP WITH TIME ZONE
);

-- Create token_transactions table for tracking balance changes
CREATE TABLE IF NOT EXISTS billing.token_transactions (
    id SERIAL PRIMARY KEY,
    user_id UUID NOT NULL REFERENCES billing.users(user_id),
    amount BIGINT NOT NULL,
    description TEXT,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- Create indexes for performance
CREATE INDEX IF NOT EXISTS idx_users_email ON billing.users(email);
CREATE INDEX IF NOT EXISTS idx_users_subscription ON billing.users(subscription_tier);
CREATE INDEX IF NOT EXISTS idx_transactions_user_id ON billing.token_transactions(user_id);
CREATE INDEX IF NOT EXISTS idx_transactions_created_at ON billing.token_transactions(created_at);

-- Create updated_at trigger function
CREATE OR REPLACE FUNCTION billing.updated_at_trigger()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ language 'plpgsql';

-- Create trigger for users table
CREATE TRIGGER users_updated_at
    BEFORE UPDATE ON billing.users
    FOR EACH ROW
    EXECUTE FUNCTION billing.updated_at_trigger();
EOF

        cat > "$MIGRATIONS_DIR/000001_initial_schema.down.sql" << 'EOF'
-- Down migration for initial schema
-- This removes the initial schema changes

-- Drop triggers
DROP TRIGGER IF EXISTS users_updated_at ON billing.users;

-- Drop trigger function
DROP FUNCTION IF EXISTS billing.updated_at_trigger();

-- Drop indexes
DROP INDEX IF EXISTS idx_transactions_created_at;
DROP INDEX IF EXISTS idx_transactions_user_id;
DROP INDEX IF EXISTS idx_users_subscription;
DROP INDEX IF EXISTS idx_users_email;

-- Drop tables
DROP TABLE IF EXISTS billing.token_transactions;
DROP TABLE IF EXISTS billing.users;

-- Drop schema
DROP SCHEMA IF EXISTS billing;
EOF

        log_info "Created initial migration template at 000001_initial_schema.up.sql"
    fi
}

# Create a new migration
create_migration() {
    local migration_name="$1"
    if [ -z "$migration_name" ]; then
        log_error "Migration name is required"
        log_info "Usage: $0 create <migration_name>"
        exit 1
    fi

    log_info "Creating new migration: $migration_name"

    # Find the next migration number
    local last_migration=$(ls -1 "$MIGRATIONS_DIR"/*.up.sql 2>/dev/null | tail -1 | grep -o '[0-9]*' | head -1)
    local next_number=$((last_migration + 1))

    # Pad with zeros to 6 digits
    local migration_number=$(printf "%06d" $next_number)

    local up_file="$MIGRATIONS_DIR/${migration_number}_${migration_name}.up.sql"
    local down_file="$MIGRATIONS_DIR/${migration_number}_${migration_name}.down.sql"

    # Create migration files with templates
    cat > "$up_file" << EOF
-- Up migration: $migration_name
-- Add your SQL statements here

EOF

    cat > "$down_file" << EOF
-- Down migration: $migration_name
-- Add your rollback SQL statements here

EOF

    log_info "Created migration files:"
    log_info "  Up:   $up_file"
    log_info "  Down: $down_file"
}

# Run database migrations up
migrate_up() {
    log_info "Running database migrations up..."
    get_database_url

    cd "$MIGRATIONS_DIR"
    migrate -path . -database "$DATABASE_URL" up
    log_info "Migrations completed successfully"
}

# Run database migrations down by one step
migrate_down() {
    log_info "Running database migrations down by one step..."
    get_database_url

    cd "$MIGRATIONS_DIR"
    migrate -path . -database "$DATABASE_URL" down 1
    log_info "Down migration completed successfully"
}

# Get current migration version
migrate_version() {
    log_info "Getting current migration version..."
    get_database_url

    cd "$MIGRATIONS_DIR"
    migrate -path . -database "$DATABASE_URL" version
}

# Force migration to specific version
migrate_force() {
    local version="$1"
    if [ -z "$version" ]; then
        log_error "Version is required"
        log_info "Usage: $0 force <version>"
        exit 1
    fi

    log_warn "Forcing migration to version $version (this should be used with caution)..."
    get_database_url

    cd "$MIGRATIONS_DIR"
    migrate -path . -database "$DATABASE_URL" force "$version"
    log_info "Migration forced to version $version"
}

# Show migration help
show_help() {
    echo "Database Migration Script for Billing Service"
    echo ""
    echo "Usage: $0 <command> [options]"
    echo ""
    echo "Commands:"
    echo "  init              Initialize migrations directory"
    echo "  create <name>     Create a new migration with the given name"
    echo "  up                Run all pending migrations"
    echo "  down              Rollback one migration"
    echo "  version           Show current migration version"
    echo "  force <version>   Force migration to specific version (use with caution)"
    echo "  help              Show this help message"
    echo ""
    echo "Examples:"
    echo "  $0 init"
    echo "  $0 create add_user_profile_table"
    echo "  $0 up"
    echo "  $0 down"
    echo "  $0 version"
    echo ""
    echo "Environment Variables:"
    echo "  DATABASE_URL              Database connection URL"
    echo "  DATABASE_URL_SECRET_NAME  Secret Manager secret name for database URL"
}

# Main script logic
case "${1:-}" in
    "init")
        init_migrations
        ;;
    "create")
        create_migration "$2"
        ;;
    "up")
        migrate_up
        ;;
    "down")
        migrate_down
        ;;
    "version")
        migrate_version
        ;;
    "force")
        migrate_force "$2"
        ;;
    "help"|"--help"|"-h"|"")
        show_help
        ;;
    *)
        log_error "Unknown command: $1"
        show_help
        exit 1
        ;;
esac