CREATE TABLE IF NOT EXISTS entries (
	id uuid DEFAULT gen_random_uuid() PRIMARY KEY,
	parent uuid REFERENCES entries (id),
	name varchar(255) NOT NULL,
	description varchar(255),
	lms_location_code varchar(255),
	contact_name varchar(255),
	email varchar(255),
	phone varchar(255)
);

CREATE TABLE IF NOT EXISTS symbols (
	id uuid DEFAULT gen_random_uuid() PRIMARY KEY,
	owner uuid NOT NULL REFERENCES entries (id) ON DELETE CASCADE,
	authority varchar(255) NOT NULL CHECK (symbol = upper(symbol)),
	symbol varchar(255) NOT NULL CHECK (symbol = upper(symbol)),
	UNIQUE (authority, symbol)
);

CREATE INDEX symbols_authority_symbol_idx ON symbols (symbol, authority);

CREATE TABLE IF NOT EXISTS service_endpoints (
	id uuid DEFAULT gen_random_uuid() PRIMARY KEY,
	entry uuid NOT NULL REFERENCES entries (id) ON DELETE CASCADE,
	name varchar(255) NOT NULL,
	type varchar(255) NOT NULL,
	address varchar(255) NOT NULL
);

CREATE TABLE IF NOT EXISTS consortia (
	id uuid DEFAULT gen_random_uuid() PRIMARY KEY,
	entry uuid REFERENCES entries (id) ON DELETE SET NULL,
	name varchar(255) NOT NULL
);

CREATE TABLE IF NOT EXISTS tiers (
	id uuid DEFAULT gen_random_uuid() PRIMARY KEY,
	name varchar(255)
);

CREATE TABLE IF NOT EXISTS networks (
	id uuid DEFAULT gen_random_uuid() PRIMARY KEY,
	name varchar(255)
);

CREATE TABLE IF NOT EXISTS memberships (
	id uuid DEFAULT gen_random_uuid() PRIMARY KEY,
	institution uuid NOT NULL REFERENCES entries (id),
	consortium uuid NOT NULL REFERENCES consortia (id)
);

CREATE TABLE IF NOT EXISTS membership_tiers (
	id uuid DEFAULT gen_random_uuid() PRIMARY KEY,
	membership uuid NOT NULL REFERENCES memberships (id),
	tier uuid NOT NULL REFERENCES tiers (id)
);

CREATE TABLE IF NOT EXISTS membership_networks (
	id uuid DEFAULT gen_random_uuid() PRIMARY KEY,
	membership uuid NOT NULL REFERENCES memberships (id),
	network uuid NOT NULL REFERENCES networks (id)
);
