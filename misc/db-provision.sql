-- Create login role
CREATE ROLE crosslink LOGIN PASSWORD 'REPLACE_WITH_SECRET';

-- Allow this role to connect to the database
GRANT CONNECT ON DATABASE "DB_NAME" TO crosslink;

-- Create schema owned by crosslink
-- Note: if schema already exists (migration) ensure USAGE and CREATE are granted for future and existing objects
CREATE SCHEMA IF NOT EXISTS crosslink_broker AUTHORIZATION crosslink;

-- Prevent crosslink from using or creating in public schema
REVOKE ALL ON SCHEMA public FROM crosslink;

-- Set default schema for this role
ALTER ROLE crosslink IN DATABASE "DB_NAME"
SET search_path = crosslink_broker, pg_temp;
