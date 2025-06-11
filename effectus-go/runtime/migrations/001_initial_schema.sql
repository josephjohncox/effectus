-- +goose Up
-- Create initial schema for Effectus rule storage

-- Enable necessary extensions
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";
CREATE EXTENSION IF NOT EXISTS "pg_trgm";

-- Create enum types
CREATE TYPE ruleset_status AS ENUM (
    'draft',
    'validating', 
    'ready',
    'deployed',
    'deprecated',
    'failed'
);

CREATE TYPE deployment_status AS ENUM (
    'pending',
    'deploying',
    'active',
    'canary',
    'rolling_back',
    'failed',
    'inactive'
);

CREATE TYPE rule_type AS ENUM (
    'list',
    'flow'
);

-- Main rulesets table
CREATE TABLE rulesets (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    name VARCHAR(255) NOT NULL,
    version VARCHAR(100) NOT NULL,
    environment VARCHAR(100) NOT NULL,
    status ruleset_status NOT NULL DEFAULT 'draft',
    
    -- Rule data
    ruleset_data JSONB NOT NULL,
    rule_count INTEGER NOT NULL DEFAULT 0,
    
    -- Metadata
    description TEXT,
    tags TEXT[],
    owner VARCHAR(255),
    team VARCHAR(255),
    metadata JSONB DEFAULT '{}',
    
    -- Git integration
    git_commit VARCHAR(255),
    git_branch VARCHAR(255),
    git_tag VARCHAR(255),
    git_author VARCHAR(255),
    pull_request VARCHAR(255),
    
    -- Compilation info
    compiled_at TIMESTAMPTZ,
    compiler_version VARCHAR(100),
    schema_version VARCHAR(100),
    validation_hash VARCHAR(255),
    
    -- Timestamps
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_by VARCHAR(255),
    updated_by VARCHAR(255),
    
    -- Constraints
    CONSTRAINT unique_name_version_env UNIQUE (name, version, environment),
    CONSTRAINT valid_rule_count CHECK (rule_count >= 0)
);

-- Deployments table
CREATE TABLE deployments (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    ruleset_id UUID NOT NULL REFERENCES rulesets(id) ON DELETE CASCADE,
    
    -- Deployment info
    environment VARCHAR(100) NOT NULL,
    status deployment_status NOT NULL DEFAULT 'pending',
    strategy VARCHAR(50) NOT NULL DEFAULT 'rolling',
    
    -- Configuration
    config JSONB DEFAULT '{}',
    health_check JSONB DEFAULT '{}',
    rollback_info JSONB DEFAULT '{}',
    canary_config JSONB DEFAULT '{}',
    
    -- Timestamps and actors
    deployed_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    completed_at TIMESTAMPTZ,
    deployed_by VARCHAR(255),
    
    -- Performance tracking
    deployment_duration_ms INTEGER,
    
    -- Constraints
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    
    CONSTRAINT unique_ruleset_environment UNIQUE (ruleset_id, environment)
);

-- Environment configurations
CREATE TABLE environments (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    name VARCHAR(100) NOT NULL UNIQUE,
    type VARCHAR(50) NOT NULL, -- development, staging, production
    
    -- Environment configuration
    config JSONB DEFAULT '{}',
    constraints JSONB DEFAULT '{}',
    approvers TEXT[],
    
    -- Settings
    auto_deploy BOOLEAN DEFAULT false,
    require_approval BOOLEAN DEFAULT false,
    max_rule_complexity INTEGER DEFAULT 100,
    performance_budget_ms INTEGER DEFAULT 1000,
    
    -- Timestamps
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Audit log table
CREATE TABLE audit_log (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    
    -- Action details
    action VARCHAR(100) NOT NULL,
    resource VARCHAR(255) NOT NULL,
    resource_id UUID, -- FK to rulesets.id when applicable
    version VARCHAR(100),
    environment VARCHAR(100),
    
    -- Actor information
    user_id VARCHAR(255),
    user_email VARCHAR(255),
    ip_address INET,
    user_agent TEXT,
    session_id VARCHAR(255),
    
    -- Request details
    details JSONB DEFAULT '{}',
    request_id VARCHAR(255),
    trace_id VARCHAR(255),
    
    -- Result
    result VARCHAR(50) NOT NULL, -- success, failure, error
    error_message TEXT,
    duration_ms INTEGER,
    
    -- Timestamp
    timestamp TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    
    -- Partitioning helper
    month_partition INTEGER GENERATED ALWAYS AS (EXTRACT(YEAR FROM timestamp) * 100 + EXTRACT(MONTH FROM timestamp)) STORED
);

-- Rule execution metrics (for monitoring)
CREATE TABLE rule_metrics (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    ruleset_id UUID NOT NULL REFERENCES rulesets(id) ON DELETE CASCADE,
    
    -- Execution stats
    execution_count BIGINT DEFAULT 0,
    success_count BIGINT DEFAULT 0,
    error_count BIGINT DEFAULT 0,
    
    -- Performance metrics
    total_duration_ms BIGINT DEFAULT 0,
    min_duration_ms INTEGER DEFAULT 0,
    max_duration_ms INTEGER DEFAULT 0,
    avg_duration_ms INTEGER DEFAULT 0,
    
    -- Resource usage
    memory_usage_mb INTEGER DEFAULT 0,
    cpu_usage_percent NUMERIC(5,2) DEFAULT 0,
    
    -- Timestamps
    last_executed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    
    CONSTRAINT unique_ruleset_metrics UNIQUE (ruleset_id)
);

-- Indexes for performance

-- Rulesets indexes
CREATE INDEX idx_rulesets_name ON rulesets(name);
CREATE INDEX idx_rulesets_version ON rulesets(name, version);
CREATE INDEX idx_rulesets_environment ON rulesets(environment);
CREATE INDEX idx_rulesets_status ON rulesets(status);
CREATE INDEX idx_rulesets_owner ON rulesets(owner);
CREATE INDEX idx_rulesets_team ON rulesets(team);
CREATE INDEX idx_rulesets_created_at ON rulesets(created_at);
CREATE INDEX idx_rulesets_git_commit ON rulesets(git_commit);
CREATE INDEX idx_rulesets_tags ON rulesets USING GIN(tags);
CREATE INDEX idx_rulesets_metadata ON rulesets USING GIN(metadata);

-- Deployments indexes
CREATE INDEX idx_deployments_environment ON deployments(environment);
CREATE INDEX idx_deployments_status ON deployments(status);
CREATE INDEX idx_deployments_deployed_at ON deployments(deployed_at);
CREATE INDEX idx_deployments_ruleset_id ON deployments(ruleset_id);

-- Audit log indexes
CREATE INDEX idx_audit_log_timestamp ON audit_log(timestamp);
CREATE INDEX idx_audit_log_action ON audit_log(action);
CREATE INDEX idx_audit_log_resource ON audit_log(resource);
CREATE INDEX idx_audit_log_user_id ON audit_log(user_id);
CREATE INDEX idx_audit_log_result ON audit_log(result);
CREATE INDEX idx_audit_log_month_partition ON audit_log(month_partition);
CREATE INDEX idx_audit_log_resource_id ON audit_log(resource_id);

-- Metrics indexes
CREATE INDEX idx_rule_metrics_ruleset_id ON rule_metrics(ruleset_id);
CREATE INDEX idx_rule_metrics_last_executed ON rule_metrics(last_executed_at);

-- Triggers for updated_at columns
CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = CURRENT_TIMESTAMP;
    RETURN NEW;
END;
$$ language 'plpgsql';

CREATE TRIGGER update_rulesets_updated_at
    BEFORE UPDATE ON rulesets
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER update_deployments_updated_at
    BEFORE UPDATE ON deployments
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER update_environments_updated_at
    BEFORE UPDATE ON environments
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER update_rule_metrics_updated_at
    BEFORE UPDATE ON rule_metrics
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

-- Insert default environments
INSERT INTO environments (name, type, config, require_approval, auto_deploy) VALUES
('development', 'development', '{"storage_type": "postgres", "hot_reload": true}', false, true),
('staging', 'staging', '{"storage_type": "postgres", "health_checks": true}', false, false),
('production', 'production', '{"storage_type": "postgres", "strategy": "canary"}', true, false);

-- +goose Down
-- Drop everything in reverse order

DROP TRIGGER IF EXISTS update_rule_metrics_updated_at ON rule_metrics;
DROP TRIGGER IF EXISTS update_environments_updated_at ON environments;
DROP TRIGGER IF EXISTS update_deployments_updated_at ON deployments;
DROP TRIGGER IF EXISTS update_rulesets_updated_at ON rulesets;

DROP FUNCTION IF EXISTS update_updated_at_column();

DROP TABLE IF EXISTS rule_metrics;
DROP TABLE IF EXISTS audit_log;
DROP TABLE IF EXISTS environments;
DROP TABLE IF EXISTS deployments;
DROP TABLE IF EXISTS rulesets;

DROP TYPE IF EXISTS rule_type;
DROP TYPE IF EXISTS deployment_status;
DROP TYPE IF EXISTS ruleset_status; 