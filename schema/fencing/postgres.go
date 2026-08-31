package fencing

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/josephjohncox/effectus/invocation"
)

// PostgresProvider issues durable monotonic tokens from V2 migration tables.
type PostgresProvider struct {
	db *sql.DB
}

func NewPostgresProvider(db *sql.DB) (*PostgresProvider, error) {
	if db == nil {
		return nil, fmt.Errorf("PostgreSQL fencing database is required")
	}
	return &PostgresProvider{db: db}, nil
}

func (*PostgresProvider) Guarantee() Guarantee { return GuaranteeDurableMonotonic }

func (provider *PostgresProvider) Acquire(ctx context.Context, request Request) (Lease, error) {
	if err := validateRequest(request); err != nil {
		return nil, err
	}
	var last error
	for attempt := 0; attempt < 5; attempt++ {
		lease, err := provider.acquireOnce(ctx, request)
		if err == nil {
			return lease, nil
		}
		if !IsSerializationFailure(err) {
			return nil, err
		}
		last = err
		delay := time.Duration(attempt+1) * 10 * time.Millisecond
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
	return nil, fmt.Errorf("fencing acquisition exhausted serialization retries: %w", last)
}

func (provider *PostgresProvider) acquireOnce(ctx context.Context, request Request) (Lease, error) {
	tx, err := provider.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return nil, fmt.Errorf("begin fencing acquisition: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var token uint64
	err = tx.QueryRowContext(ctx, `
		INSERT INTO effectus_fencing_counters (authority, resource, token)
		VALUES ($1, $2, 1)
		ON CONFLICT (authority, resource) DO UPDATE
		SET token = effectus_fencing_counters.token + 1
		RETURNING token
	`, request.Authority, request.Resource).Scan(&token)
	if err != nil {
		return nil, fmt.Errorf("issue fencing token: %w", err)
	}

	var held bool
	err = tx.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM effectus_fencing_leases
			WHERE authority = $1 AND resource = $2 AND expires_at > now()
		)
	`, request.Authority, request.Resource).Scan(&held)
	if err != nil {
		return nil, fmt.Errorf("check fencing lease: %w", err)
	}
	if held {
		return nil, fmt.Errorf("%w: %s/%s", ErrLeaseHeld, request.Authority, request.Resource)
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO effectus_fencing_leases (authority, resource, holder, token, expires_at, revision)
		VALUES ($1, $2, $3, $4, now() + ($5 * interval '1 microsecond'), 1)
		ON CONFLICT (authority, resource) DO UPDATE
		SET holder = EXCLUDED.holder,
		    token = EXCLUDED.token,
		    expires_at = EXCLUDED.expires_at,
		    revision = effectus_fencing_leases.revision + 1
	`, request.Authority, request.Resource, request.Holder, token, request.TTL.Microseconds())
	if err != nil {
		return nil, fmt.Errorf("persist fencing lease: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit fencing acquisition: %w", err)
	}
	return &postgresLease{
		provider: provider, authority: request.Authority, resource: request.Resource,
		holder: request.Holder, token: token,
	}, nil
}

type postgresLease struct {
	provider  *PostgresProvider
	authority string
	resource  string
	holder    string
	token     uint64
}

func (lease *postgresLease) Grant() invocation.FencingGrant {
	return invocation.FencingGrant{Authority: lease.authority, Resource: lease.resource, Token: lease.token}
}

func (lease *postgresLease) Renew(ctx context.Context, ttl time.Duration) error {
	if ttl <= 0 {
		return fmt.Errorf("fencing lease TTL must be positive")
	}
	result, err := lease.provider.db.ExecContext(ctx, `
		UPDATE effectus_fencing_leases
		SET expires_at = now() + ($5 * interval '1 microsecond'), revision = revision + 1
		WHERE authority = $1 AND resource = $2 AND holder = $3 AND token = $4
		  AND expires_at > now()
	`, lease.authority, lease.resource, lease.holder, lease.token, ttl.Microseconds())
	if err != nil {
		return fmt.Errorf("renew fencing lease: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows != 1 {
		return ErrStaleGrant
	}
	return nil
}

func (lease *postgresLease) Release(ctx context.Context) error {
	result, err := lease.provider.db.ExecContext(ctx, `
		DELETE FROM effectus_fencing_leases
		WHERE authority = $1 AND resource = $2 AND holder = $3 AND token = $4
	`, lease.authority, lease.resource, lease.holder, lease.token)
	if err != nil {
		return fmt.Errorf("release fencing lease: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows != 1 {
		return ErrStaleGrant
	}
	return nil
}

// IsSerializationFailure reports the PostgreSQL retry class without coupling
// callers to one concrete driver error type.
func IsSerializationFailure(err error) bool {
	if err == nil {
		return false
	}
	var sqlState interface{ SQLState() string }
	return errors.As(err, &sqlState) && sqlState.SQLState() == "40001"
}
