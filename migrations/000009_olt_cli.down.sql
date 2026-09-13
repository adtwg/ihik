ALTER TABLE olts
    DROP COLUMN IF EXISTS cli_protocol,
    DROP COLUMN IF EXISTS cli_port,
    DROP COLUMN IF EXISTS cli_username,
    DROP COLUMN IF EXISTS cli_password_ciphertext,
    DROP COLUMN IF EXISTS cli_enable_password_ciphertext;
