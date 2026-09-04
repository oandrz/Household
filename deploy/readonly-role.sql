-- Creates the SELECT-only role the admin database browse reads through.
--
-- Idempotent: the dev Compose one-shot runs it on every `make dev` and the Go
-- suite runs it per test container. Postgres has no CREATE ROLE IF NOT
-- EXISTS, hence the DO block; the GRANTs are naturally idempotent.
--
-- Run as a role that may create roles and that owns the tables (`hearth` in
-- every environment today).
--
-- It sets no password, deliberately: psql's :'var' interpolation is a
-- client-side feature, and a file using it is a syntax error when the Go
-- suite sends it to the server through pgx. Every consumer sets the password
-- itself in one statement afterwards -- see the spec's decision 5.

DO $$
BEGIN
    IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'hearth_readonly') THEN
        CREATE ROLE hearth_readonly LOGIN;
    END IF;
END
$$;

-- Read-only twice over: once by what is granted, once by what the role's own
-- session default allows. The two fail independently, and the second survives
-- an adapter that forgets to open a transaction -- SET LOCAL would not. See
-- the spec's decision 3.
ALTER ROLE hearth_readonly SET default_transaction_read_only = on;
ALTER ROLE hearth_readonly SET statement_timeout = '3s';

GRANT CONNECT ON DATABASE hearth TO hearth_readonly;
GRANT USAGE ON SCHEMA public TO hearth_readonly;
GRANT SELECT ON ALL TABLES IN SCHEMA public TO hearth_readonly;

-- GRANT ... ON ALL TABLES covers only the tables that exist at the moment it
-- runs. Without this line, the table migration 00014 creates is invisible to
-- the browse -- information_schema lists only what the current role may see --
-- and nothing anywhere reports it. FOR ROLE hearth is load-bearing: default
-- privileges attach to the role that creates the object, and migrations run
-- as hearth. See the spec's decision 2.
ALTER DEFAULT PRIVILEGES FOR ROLE hearth IN SCHEMA public
    GRANT SELECT ON TABLES TO hearth_readonly;
