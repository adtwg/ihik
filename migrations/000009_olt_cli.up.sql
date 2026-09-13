ALTER TABLE olts
    ADD COLUMN IF NOT EXISTS cli_protocol text NOT NULL DEFAULT 'ssh' CHECK (cli_protocol IN ('ssh', 'telnet')),
    ADD COLUMN IF NOT EXISTS cli_port integer NOT NULL DEFAULT 22 CHECK (cli_port BETWEEN 1 AND 65535),
    ADD COLUMN IF NOT EXISTS cli_username text,
    ADD COLUMN IF NOT EXISTS cli_password_ciphertext bytea,
    ADD COLUMN IF NOT EXISTS cli_enable_password_ciphertext bytea;
