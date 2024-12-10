BEGIN;

CREATE TABLE entries (
	id uuid DEFAULT gen_random_uuid() PRIMARY KEY,
	parent uuid REFERENCES entries (id),
	name varchar(255) NOT NULL,
	description varchar(255),
	lms_location_code varchar(255),
	contact_name varchar(255),
	email_address varchar(255),
	phone_number varchar(255)
);

CREATE TABLE authorities (
	id uuid DEFAULT gen_random_uuid() PRIMARY KEY,
	symbol varchar(255) NOT NULL CHECK (symbol = upper(symbol)),
	UNIQUE (symbol)
);

CREATE TABLE symbols (
	id uuid DEFAULT gen_random_uuid() PRIMARY KEY,
	owner uuid NOT NULL REFERENCES entries (id),
	authority uuid NOT NULL REFERENCES authorities (id),
	symbol varchar(255) NOT NULL CHECK (symbol = upper(symbol)),
	UNIQUE (authority, symbol)
);

-- Workaround for sqlc's poor left join handling https://github.com/sqlc-dev/sqlc/issues/2997
CREATE VIEW entrysymbols AS (
  SELECT symbols.* FROM entries LEFT JOIN symbols ON entries.id = symbols.owner
);

CREATE INDEX symbols_authority_symbol_idx ON symbols (symbol, authority);

CREATE TABLE consortia (
	id uuid DEFAULT gen_random_uuid() PRIMARY KEY,
	directory_entry uuid REFERENCES entries (id),
	name varchar(255)
);

CREATE TABLE tiers (
	id uuid DEFAULT gen_random_uuid() PRIMARY KEY,
	name varchar(255)
);

CREATE TABLE networks (
	id uuid DEFAULT gen_random_uuid() PRIMARY KEY,
	name varchar(255)
);

CREATE TABLE memberships (
	id uuid DEFAULT gen_random_uuid() PRIMARY KEY,
	institution uuid NOT NULL REFERENCES entries (id),
	consortium uuid NOT NULL REFERENCES consortia (id)
);

CREATE TABLE membership_tiers (
	id uuid DEFAULT gen_random_uuid() PRIMARY KEY,
	membership uuid NOT NULL REFERENCES memberships (id),
	tier uuid NOT NULL REFERENCES tiers (id)
);

CREATE TABLE membership_networks (
	id uuid DEFAULT gen_random_uuid() PRIMARY KEY,
	membership uuid NOT NULL REFERENCES memberships (id),
	network uuid NOT NULL REFERENCES networks (id)
);

COMMIT;
