package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/kusold/gotchi/internal/db"
)

// PostgresStoreConfig configures the [PostgresIdentityStore].
type PostgresStoreConfig struct {
	// DefaultTenantName is the name used when creating the first tenant for a
	// new user who has no existing tenant. Defaults to "Default".
	DefaultTenantName string
}

// PostgresIdentityStore implements [IdentityStore] using PostgreSQL. It stores
// users, tenants, and memberships in the database tables created by the auth
// migrations ([migrations.Auth]).
type PostgresIdentityStore struct {
	pool    *pgxpool.Pool
	queries *db.Queries
	cfg     PostgresStoreConfig
}

// NewPostgresIdentityStore creates a new PostgreSQL-backed identity store.
// The pool must be connected to a database with the auth schema migrations
// applied.
func NewPostgresIdentityStore(pool *pgxpool.Pool, cfg PostgresStoreConfig) (*PostgresIdentityStore, error) {
	conf := cfg
	if conf.DefaultTenantName == "" {
		conf.DefaultTenantName = "Default"
	}

	return &PostgresIdentityStore{
		pool:    pool,
		queries: db.New(pool),
		cfg:     conf,
	}, nil
}

// ResolveOrProvisionUser implements [IdentityStore.ResolveOrProvisionUser].
func (s *PostgresIdentityStore) ResolveOrProvisionUser(ctx context.Context, identity Identity) (UserRef, error) {
	if s.pool == nil {
		return UserRef{}, fmt.Errorf("postgres pool is required")
	}

	// Try to get existing user
	user, err := s.queries.GetUserByIdentifier(ctx, db.GetUserByIdentifierParams{
		Issuer:            identity.Issuer,
		IdentifierSubject: identity.Subject,
	})
	if err == nil {
		// User exists - verify they have at least one membership
		memberships, listErr := s.ListMemberships(ctx, user.ID)
		if listErr != nil {
			return UserRef{}, fmt.Errorf("failed to list memberships: %w", listErr)
		}
		if len(memberships) == 0 {
			// Data integrity issue - user should have at least one membership
			return UserRef{}, fmt.Errorf("user %s has no tenant memberships (data integrity issue)", user.ID)
		}
		return UserRef{
			UserID:  user.ID,
			Issuer:  user.Issuer,
			Subject: user.IdentifierSubject,
		}, nil
	}

	// Check if error is "no rows" - if so, create new user
	if err != pgx.ErrNoRows {
		return UserRef{}, fmt.Errorf("failed to query user: %w", err)
	}

	// Create new user
	tenantID, err := s.firstTenantOrCreate(ctx)
	if err != nil {
		return UserRef{}, fmt.Errorf("failed to get or create tenant: %w", err)
	}

	userID, err := uuid.NewV7()
	if err != nil {
		userID = uuid.New()
	}

	username := identity.PreferredUsername
	if username == "" {
		username = identity.Username
	}

	now := time.Now()
	created, err := s.queries.InsertUser(ctx, db.InsertUserParams{
		ID:                userID,
		Email:             identity.Email,
		EmailVerified:     identity.EmailVerified,
		Username:          pgtype.Text{String: username, Valid: username != ""},
		Name:              pgtype.Text{String: identity.Name, Valid: identity.Name != ""},
		Issuer:            identity.Issuer,
		IdentifierSubject: identity.Subject,
		LastLoginAt:       pgtype.Timestamptz{Time: now, Valid: true},
	})
	if err != nil {
		return UserRef{}, fmt.Errorf("failed to insert user: %w", err)
	}

	if _, err := s.createMembership(ctx, created.ID, tenantID, RoleMember); err != nil {
		return UserRef{}, fmt.Errorf("failed to create membership: %w", err)
	}

	memberships, err := s.ListMemberships(ctx, created.ID)
	if err != nil {
		return UserRef{}, fmt.Errorf("failed to list memberships for created user: %w", err)
	}
	if len(memberships) == 0 {
		return UserRef{}, fmt.Errorf("failed to verify membership creation")
	}

	return UserRef{
		UserID:  created.ID,
		Issuer:  created.Issuer,
		Subject: created.IdentifierSubject,
	}, nil
}

// ListMemberships implements [IdentityStore.ListMemberships].
func (s *PostgresIdentityStore) ListMemberships(ctx context.Context, userID uuid.UUID) ([]Membership, error) {
	rows, err := s.queries.ListMemberships(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to list memberships: %w", err)
	}

	memberships := make([]Membership, len(rows))
	for i, row := range rows {
		memberships[i] = Membership{
			TenantID:   row.TenantID,
			TenantName: row.TenantName,
			Role:       Role(row.Role),
		}
	}
	return memberships, nil
}

// GetTenantDisplay implements [IdentityStore.GetTenantDisplay].
func (s *PostgresIdentityStore) GetTenantDisplay(ctx context.Context, tenantID uuid.UUID) (TenantDisplay, error) {
	row, err := s.queries.GetTenantByID(ctx, tenantID)
	if err != nil {
		return TenantDisplay{}, fmt.Errorf("failed to get tenant: %w", err)
	}
	return TenantDisplay{
		TenantID: row.TenantID,
		Name:     row.Name,
	}, nil
}

func (s *PostgresIdentityStore) firstTenantOrCreate(ctx context.Context) (uuid.UUID, error) {
	tenantID, err := s.queries.GetFirstTenant(ctx)
	if err == nil {
		return tenantID, nil
	}

	// Check if error is "no rows"
	if !errors.Is(err, pgx.ErrNoRows) {
		return uuid.UUID{}, fmt.Errorf("failed to query tenant: %w", err)
	}

	// Create new tenant
	newTenantID, genErr := uuid.NewV7()
	if genErr != nil {
		newTenantID = uuid.New()
	}

	if err := s.queries.InsertTenant(ctx, db.InsertTenantParams{
		TenantID: newTenantID,
		Name:     s.cfg.DefaultTenantName,
	}); err != nil {
		return uuid.UUID{}, fmt.Errorf("failed to insert tenant: %w", err)
	}
	return newTenantID, nil
}

func (s *PostgresIdentityStore) createMembership(ctx context.Context, userID, tenantID uuid.UUID, role Role) (Membership, error) {
	row, err := s.queries.UpsertMembership(ctx, db.UpsertMembershipParams{
		UserID:   userID,
		TenantID: tenantID,
		Role:     string(role),
	})
	if err != nil {
		return Membership{}, fmt.Errorf("failed to upsert membership: %w", err)
	}

	m := Membership{
		TenantID: row.TenantID,
		Role:     Role(row.Role),
	}

	// Get tenant name
	display, err := s.GetTenantDisplay(ctx, m.TenantID)
	if err == nil {
		m.TenantName = display.Name
	}
	return m, nil
}
