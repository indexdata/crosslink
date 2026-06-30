CREATE TABLE IF NOT EXISTS entries (
	id uuid DEFAULT gen_random_uuid() PRIMARY KEY,
	parent uuid REFERENCES entries (id),
	name varchar(255) NOT NULL,
	type varchar(255) NOT NULL,
	description varchar(255),
	organization_id varchar(255),
	contact_name varchar(255),
	email varchar(255),
	phone_number varchar(255),
	lms_location_code varchar(255),
	lender_of_last_resort varchar(255),
	hrid varchar(255) UNIQUE,
	time_zone varchar(128)
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
	address text NOT NULL
);

CREATE TABLE IF NOT EXISTS addresses (
	id uuid DEFAULT gen_random_uuid() PRIMARY KEY,
	entry uuid NOT NULL REFERENCES entries (id) ON DELETE CASCADE,
	type text NOT NULL
);

CREATE TABLE IF NOT EXISTS address_components (
	id uuid DEFAULT gen_random_uuid() PRIMARY KEY,
	address uuid NOT NULL REFERENCES addresses (id) ON DELETE CASCADE,
	seq integer NOT NULL,
	type text NOT NULL,
	value text NOT NULL
);

CREATE TABLE IF NOT EXISTS closures (
	id uuid DEFAULT gen_random_uuid() PRIMARY KEY,
	entry uuid NOT NULL references entries (id) ON DELETE CASCADE,
	start_date timestamp NOT NULL,
	end_date timestamp NOT NULL,
	reason text NOT NULL
);

CREATE TABLE IF NOT EXISTS tiers (
	id uuid DEFAULT gen_random_uuid() PRIMARY KEY,
	consortium uuid NOT NULL REFERENCES entries (id) ON DELETE CASCADE,
	name varchar(255),
	level varchar(32) NOT NULL DEFAULT 'standard' CHECK (level IN ('express', 'normal', 'rush', 'secondarymail', 'standard', 'urgent')),
	type varchar(32) NOT NULL DEFAULT 'loan' CHECK (type IN ('loan', 'copy')),
	cost double precision NOT NULL DEFAULT 0.0
);

CREATE INDEX tiers_consortium_idx ON tiers (consortium);

CREATE TABLE IF NOT EXISTS networks (
	id uuid DEFAULT gen_random_uuid() PRIMARY KEY,
	consortium uuid NOT NULL REFERENCES entries (id) ON DELETE CASCADE,
	name varchar(255),
	priority double precision NOT NULL DEFAULT 0.0
);

CREATE INDEX networks_consortium_idx ON networks (consortium);

CREATE TABLE IF NOT EXISTS entry_tiers (
	id uuid DEFAULT gen_random_uuid() PRIMARY KEY,
	entry uuid NOT NULL REFERENCES entries (id) ON DELETE CASCADE,
	tier uuid NOT NULL REFERENCES tiers (id)
);

CREATE TABLE IF NOT EXISTS entry_networks (
	id uuid DEFAULT gen_random_uuid() PRIMARY KEY,
	entry uuid NOT NULL REFERENCES entries (id) ON DELETE CASCADE,
	network uuid NOT NULL REFERENCES networks (id)
);

CREATE TABLE IF NOT EXISTS lms_configs (
	id uuid DEFAULT gen_random_uuid() PRIMARY KEY,
	entry uuid NOT NULL REFERENCES entries (id) ON DELETE CASCADE UNIQUE,
	address text NOT NULL,
	from_agency varchar(128) NOT NULL,
	from_agency_authentication text,
	to_agency varchar(128),
	lookup_user_enabled boolean,
	accept_item_enabled boolean,
	checkin_item_enabled boolean,
	checkout_item_enabled boolean,
	item_location varchar(255),
	request_item_request_type varchar(32),
	request_item_scope_type varchar(32),
	request_item_bib_code varchar(128),
	request_item_pickup_location_enabled boolean,
	requester_pickup_location varchar(128),
	supplier_pickup_location varchar(128),
	requester_patron_pattern varchar(255)
);
