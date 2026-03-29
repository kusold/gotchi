package auth

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresStoreConfig struct {
	DefaultTenantName string
}

type PostgresIdentityStore struct {
	pool *pgxpool.Pool
	cfg  PostgresStoreConfig
}

func NewPostgresIdentityStore(pool *pgxpool.Pool, cfg PostgresStoreConfig) (*PostgresIdentityStore, error) {
	conf := cfg
	if conf.DefaultTenantName == "" {
		conf.DefaultTenantName = "Default"
	}
	return &PostgresIdentityStore{pool: pool, cfg: conf}, nil
}

func (s *PostgresIdentityStore) ResolveOrProvisionUser(ctx context.Context, identity Identity) (UserRef, error) {
	if s.pool == nil {
		return UserRef{}, fmt.Errorf("postgres pool is required")
	}

	queryUser := `
		SELECT id, issuer, identifier_subject, COALESCE(tenant_id, '00000000-0000-0000-0000-000000000000'::uuid)
		FROM users
		WHERE issuer = $1 AND identifier_subject = $2`

	var user UserRef
	var fallbackTenantID uuid.UUID
	err := s.pool.QueryRow(ctx, queryUser, identity.Issuer, identity.Subject).Scan(
		&user.UserID,
		&user.Issuer,
		&user.Subject,
		&fallbackTenantID,
	)
	if err == nil {
		memberships, listErr := s.ListMemberships(ctx, user.UserID)
		if listErr != nil {
			return UserRef{}, listErr
		}
		if len(memberships) == 0 {
			if fallbackTenantID == uuid.Nil {
				return UserRef{}, fmt.Errorf("user must belong to at least one tenant")
			}
			if _, createErr := s.createMembership(ctx, user.UserID, fallbackTenantID, RoleMember); createErr != nil {
				return UserRef{}, createErr
			}
		}
		return user, nil
	}
	if err != pgx.ErrNoRows {
		return UserRef{}, err
	}

	tenantID, err := s.firstTenantOrCreate(ctx)
	if err != nil {
		return UserRef{}, err
	}

	userID, err := uuid.NewV7()
	if err != nil {
		userID = uuid.New()
	}

	username := identity.PreferredUsername
	if username == "" {
		username = identity.Username
	}

	insertUser := `
		INSERT INTO users (
			id,
			tenant_id,
			email,
			email_verified,
			username,
			name,
			issuer,
			identifier_subject,
			last_login_at
		) VALUES (
			$1,$2,$3,$4,$5,$6,$7,$8,$9
		)
		RETURNING id, issuer, identifier_subject`

	var created UserRef
	err = s.pool.QueryRow(ctx, insertUser,
		userID,
		tenantID,
		identity.Email,
		identity.EmailVerified,
		username,
		identity.Name,
		identity.Issuer,
		identity.Subject,
		time.Now(),
	).Scan(&created.UserID, &created.Issuer, &created.Subject)
	if err != nil {
		return UserRef{}, err
	}

	if _, err := s.createMembership(ctx, created.UserID, tenantID, RoleMember); err != nil {
		return UserRef{}, err
	}

	memberships, err := s.ListMemberships(ctx, created.UserID)
	if err != nil {
		return UserRef{}, err
	}
	if len(memberships) == 0 {
		return UserRef{}, fmt.Errorf("user must belong to at least one tenant")
	}

	return created, nil
}

func (s *PostgresIdentityStore) ListMemberships(ctx context.Context, userID uuid.UUID) ([]Membership, error) {
	query := `
		SELECT tm.tenant_id, t.name, tm.role
		FROM tenant_memberships tm
		JOIN tenants t ON t.tenant_id = tm.tenant_id
		WHERE tm.user_id = $1
		ORDER BY tm.created_at`

	rows, err := s.pool.Query(ctx, query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]Membership, 0)
	for rows.Next() {
		var m Membership
		if err := rows.Scan(&m.TenantID, &m.TenantName, &m.Role); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *PostgresIdentityStore) GetTenantDisplay(ctx context.Context, tenantID uuid.UUID) (TenantDisplay, error) {
	query := `SELECT tenant_id, name FROM tenants WHERE tenant_id = $1`
	var display TenantDisplay
	if err := s.pool.QueryRow(ctx, query, tenantID).Scan(&display.TenantID, &display.Name); err != nil {
		return TenantDisplay{}, err
	}
	return display, nil
}

func (s *PostgresIdentityStore) firstTenantOrCreate(ctx context.Context) (uuid.UUID, error) {
	query := `SELECT tenant_id FROM tenants ORDER BY created_at LIMIT 1`
	var tenantID uuid.UUID
	err := s.pool.QueryRow(ctx, query).Scan(&tenantID)
	if err == nil {
		return tenantID, nil
	}
	if err != pgx.ErrNoRows {
		return uuid.UUID{}, err
	}

	newTenantID, genErr := uuid.NewV7()
	if genErr != nil {
		newTenantID = uuid.New()
	}
	insert := `INSERT INTO tenants (tenant_id, name) VALUES ($1, $2)`
	if _, err := s.pool.Exec(ctx, insert, newTenantID, s.cfg.DefaultTenantName); err != nil {
		return uuid.UUID{}, err
	}
	return newTenantID, nil
}

func (s *PostgresIdentityStore) createMembership(ctx context.Context, userID, tenantID uuid.UUID, role Role) (Membership, error) {
	insert := `
		INSERT INTO tenant_memberships (user_id, tenant_id, role)
		VALUES ($1, $2, $3)
		ON CONFLICT (user_id, tenant_id)
		DO UPDATE SET role = EXCLUDED.role
		RETURNING tenant_id, role`

	var m Membership
	if err := s.pool.QueryRow(ctx, insert, userID, tenantID, role).Scan(&m.TenantID, &m.Role); err != nil {
		return Membership{}, err
	}
	display, err := s.GetTenantDisplay(ctx, m.TenantID)
	if err == nil {
		m.TenantName = display.Name
	}
	return m, nil
}
