CREATE TABLE olts (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    name text NOT NULL,
    host text NOT NULL,
    model text NOT NULL DEFAULT 'ZTE-C320',
    snmp_mode text NOT NULL DEFAULT 'v3' CHECK (snmp_mode IN ('v2c', 'v3')),
    -- v2c
    community_ciphertext bytea,
    encryption_key_version smallint NOT NULL DEFAULT 1,
    -- v3
    v3_username text,
    v3_auth_protocol text NOT NULL DEFAULT 'sha' CHECK (v3_auth_protocol IN ('md5', 'sha', 'sha256', 'sha512')),
    v3_auth_passphrase_ciphertext bytea,
    v3_priv_protocol text NOT NULL DEFAULT 'aes' CHECK (v3_priv_protocol IN ('des', 'aes', 'aes192', 'aes256')),
    v3_priv_passphrase_ciphertext bytea,
    last_connected_at timestamptz,
    last_error text,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    archived_at timestamptz,
    UNIQUE (tenant_id, id)
);

CREATE INDEX idx_olts_tenant ON olts (tenant_id) WHERE archived_at IS NULL;

-- Cache hasil pembacaan ONU agar tabel cepat dimuat tanpa menunggu SNMP walk.
CREATE TABLE olt_onus (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    olt_id uuid NOT NULL,
    FOREIGN KEY (tenant_id, olt_id) REFERENCES olts (tenant_id, id) ON DELETE CASCADE,
    index text NOT NULL,
    onu_number text,
    name text,
    serial_number text,
    description text,
    status text,
    rx_power_dbm double precision,
    tx_power_dbm double precision,
    online_at timestamptz,
    synced_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, olt_id, index)
);
