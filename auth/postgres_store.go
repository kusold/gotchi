package auth

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	// schemaNameMaxLen is PostgreSQL's maximum identifier length
	schemaNameMaxLen = 63

	// errSchemaPrefix is the prefix for all schema validation errors
	errSchemaPrefix = "invalid schema name: "
)

// schemaNameRegex validates PostgreSQL schema names:
// - Must start with a letter or underscore
// - Can contain letters, digits, and underscores
// - Max 63 characters (enforced separately)
var schemaNameRegex = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

type PostgresStoreConfig struct {
	Schema            string
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
	if err := validateSchemaName(conf.Schema); err != nil {
		return nil, err
	}
	return &PostgresIdentityStore{pool: pool, cfg: conf}, nil
}

func validateSchemaName(schema string) error {
	if schema == "" {
		return nil
	}
	if len(schema) > schemaNameMaxLen {
		return fmt.Errorf(errSchemaPrefix+"must be %d characters or less", schemaNameMaxLen)
	}
	if !schemaNameRegex.MatchString(schema) {
		return fmt.Errorf(errSchemaPrefix + "must start with a letter or underscore and contain only alphanumeric characters and underscores")
	}
	return nil
}

func (s *PostgresIdentityStore) ResolveOrProvisionUser(ctx context.Context, identity Identity) (UserRef, error) {
	if s.pool == nil {
		return UserRef{}, fmt.Errorf("postgres pool is required")
	}

	queryUser := fmt.Sprintf(`
		SELECT id, issuer, identifier_subject, COALESCE(tenant_id, '00000000-0000-0000-0000-000000000000'::uuid)
		FROM %s
		WHERE issuer = $1 AND identifier_subject = $2`, s.qualify("users"))

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

	insertUser := fmt.Sprintf(`
		INSERT INTO %s (
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
		RETURNING id, issuer, identifier_subject`, s.qualify("users"))

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
	query := fmt.Sprintf(`
		SELECT tm.tenant_id, t.name, tm.role
		FROM %s tm
		JOIN %s t ON t.tenant_id = tm.tenant_id
		WHERE tm.user_id = $1
		ORDER BY tm.created_at`,
		s.qualify("tenant_memberships"),
		s.qualify("tenants"),
	)

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
	query := fmt.Sprintf(`SELECT tenant_id, name FROM %s WHERE tenant_id = $1`, s.qualify("tenants"))
	var display TenantDisplay
	if err := s.pool.QueryRow(ctx, query, tenantID).Scan(&display.TenantID, &display.Name); err != nil {
		return TenantDisplay{}, err
	}
	return display, nil
}

func (s *PostgresIdentityStore) firstTenantOrCreate(ctx context.Context) (uuid.UUID, error) {
	query := fmt.Sprintf(`SELECT tenant_id FROM %s ORDER BY created_at LIMIT 1`, s.qualify("tenants"))
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
	insert := fmt.Sprintf(`INSERT INTO %s (tenant_id, name) VALUES ($1, $2)`, s.qualify("tenants"))
	if _, err := s.pool.Exec(ctx, insert, newTenantID, s.cfg.DefaultTenantName); err != nil {
		return uuid.UUID{}, err
	}
	return newTenantID, nil
}

func (s *PostgresIdentityStore) createMembership(ctx context.Context, userID, tenantID uuid.UUID, role Role) (Membership, error) {
	insert := fmt.Sprintf(`
		INSERT INTO %s (user_id, tenant_id, role)
		VALUES ($1, $2, $3)
		ON CONFLICT (user_id, tenant_id)
		DO UPDATE SET role = EXCLUDED.role
		RETURNING tenant_id, role`, s.qualify("tenant_memberships"))

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

func (s *PostgresIdentityStore) qualify(table string) string {
	if s.cfg.Schema == "" {
		return table
	}
	return fmt.Sprintf("%s.%s", quoteIdentifier(s.cfg.Schema), quoteIdentifier(table))
}

func quoteIdentifier(identifier string) string {
	return `"` + strings.ReplaceAll(identifier, `"`, `""`) + `"`
}
