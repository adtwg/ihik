CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE tenants (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    code text NOT NULL UNIQUE,
    name text NOT NULL,
    active boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now(),
    archived_at timestamptz
);

CREATE TABLE roles (
    code text PRIMARY KEY,
    name text NOT NULL
);

CREATE TABLE permissions (
    code text PRIMARY KEY,
    name text NOT NULL
);

CREATE TABLE role_permissions (
    role_code text NOT NULL REFERENCES roles(code),
    permission_code text NOT NULL REFERENCES permissions(code),
    PRIMARY KEY (role_code, permission_code)
);

INSERT INTO roles (code, name) VALUES
    ('super_admin', 'Super Admin'),
    ('mitra', 'Mitra');

INSERT INTO permissions (code, name) VALUES
    ('platform.manage', 'Kelola platform dan mitra'),
    ('customer.manage', 'Kelola pelanggan'),
    ('router.manage', 'Kelola router MikroTik'),
    ('package.manage', 'Kelola paket'),
    ('billing.manage', 'Kelola billing dan pembayaran'),
    ('finance.read', 'Lihat histori keuangan');

INSERT INTO role_permissions (role_code, permission_code) VALUES
    ('super_admin', 'platform.manage'),
    ('mitra', 'customer.manage'),
    ('mitra', 'router.manage'),
    ('mitra', 'package.manage'),
    ('mitra', 'billing.manage'),
    ('mitra', 'finance.read');

CREATE TABLE users (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid REFERENCES tenants(id),
    username text NOT NULL,
    normalized_username text NOT NULL UNIQUE,
    password_hash text NOT NULL,
    role_code text NOT NULL REFERENCES roles(code),
    active boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    archived_at timestamptz,
    CONSTRAINT users_role_tenant_check CHECK (
        (role_code = 'super_admin' AND tenant_id IS NULL)
        OR (role_code = 'mitra' AND tenant_id IS NOT NULL)
    )
);

CREATE INDEX users_tenant_idx ON users (tenant_id) WHERE archived_at IS NULL;

CREATE TABLE user_sessions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id uuid NOT NULL REFERENCES users(id),
    token_hash bytea NOT NULL UNIQUE,
    csrf_hash bytea NOT NULL,
    ip_address inet NOT NULL,
    user_agent text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    last_seen_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL,
    revoked_at timestamptz
);

CREATE INDEX user_sessions_active_idx ON user_sessions (user_id, expires_at) WHERE revoked_at IS NULL;

CREATE TABLE login_attempts (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    normalized_username text NOT NULL,
    ip_address inet NOT NULL,
    succeeded boolean NOT NULL,
    attempted_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX login_attempts_throttle_idx
    ON login_attempts (normalized_username, ip_address, attempted_at DESC)
    WHERE succeeded = false;

CREATE TABLE audit_logs (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    tenant_id uuid REFERENCES tenants(id),
    actor_user_id uuid REFERENCES users(id),
    action text NOT NULL,
    resource_type text NOT NULL,
    resource_id text,
    correlation_id uuid NOT NULL DEFAULT gen_random_uuid(),
    ip_address inet,
    before_data jsonb,
    after_data jsonb,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX audit_logs_tenant_created_idx ON audit_logs (tenant_id, created_at DESC);

REVOKE UPDATE, DELETE ON audit_logs FROM PUBLIC;