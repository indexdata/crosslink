BEGIN;

CREATE TABLE directory_entries (
	id uuid DEFAULT gen_random_uuid() PRIMARY KEY,
	parent uuid REFERENCES directory_entries (id),
	name varchar(255) NOT NULL,
	description varchar(255),
	lms_location_code varchar(255),
	contact_name varchar(255),
	email_address varchar(255),
	phone_number varchar(255)
);

CREATE TABLE authorities (
	id uuid DEFAULT gen_random_uuid() PRIMARY KEY,
	symbol varchar(255) NOT NULL CHECK (symbol = upper(symbol))
);

CREATE TABLE symbols (
	id uuid DEFAULT gen_random_uuid() PRIMARY KEY,
	owner uuid REFERENCES directory_entries (id),
	authority uuid NOT NULL REFERENCES authorities (id),
	symbol varchar(255) NOT NULL CHECK (symbol = upper(symbol)),
	UNIQUE (authority, symbol)
);

CREATE INDEX symbols_authority_symbol_idx ON symbols (authority, symbol);

CREATE TABLE consortia (
	id uuid DEFAULT gen_random_uuid() PRIMARY KEY,
	directory_entry uuid REFERENCES directory_entries (id),
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
	institution uuid NOT NULL REFERENCES directory_entries (id),
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
