-- name: EntryByIdForUpdate :one
SELECT * FROM entries WHERE id = $1 LIMIT 1 FOR UPDATE;

-- name: EntryBySymbolForUpdate :one
SELECT e.* FROM entries e, symbols s WHERE e.id = s.owner AND s.authority = @authority AND s.symbol = @symbol LIMIT 1 FOR UPDATE OF e;

-- name: EntryBySymbol :one
SELECT e.* FROM entries e, symbols s WHERE e.id = s.owner AND s.authority = @authority AND s.symbol = @symbol LIMIT 1;

-- name: CreateEntry :one
INSERT INTO entries (
  name, description, contact_name, email, phone_number, time_zone, organization_id, type, parent, lms_location_code 
) VALUES (
  $1, $2, $3, $4, $5, $6, $7, $8, $9, $10
)
RETURNING *;

-- name: UpdateEntry :exec
UPDATE entries
SET
  name = @name,
  description = @description,
  contact_name = @contact_name,
  email = @email,
  phone_number = @phone_number,
  time_zone = @time_zone,
  organization_id = @organization_id,
  type = @type,
  parent = @parent,
  lms_location_code = @lms_location_code,
  hrid = @hrid

WHERE id = @id;

-- name: DeleteEntryById :exec
DELETE from entries WHERE id = @id;

-- name: DeleteEntryBySymbol :exec
DELETE from entries USING symbols
WHERE entries.id = symbols.owner
  AND symbols.authority = @authority
  AND symbols.symbol = @symbol;

-- name: UpsertSymbol :one
INSERT INTO symbols (
  id, owner, symbol, authority
) VALUES (
  coalesce(sqlc.narg('id'), gen_random_uuid()),
  @owner,
  @symbol,
  @authority
)
ON CONFLICT (id) DO UPDATE SET
  owner = @owner,
  symbol = @symbol,
  authority = @authority
WHERE symbols.id = sqlc.narg('id')
RETURNING *;

-- name: DeleteOtherOwnedSymbols :exec
DELETE FROM symbols WHERE owner = @owner AND ID <> ALL(@ids::uuid[]);

-- name: DeleteAllOwnedSymbols :exec
DELETE FROM symbols WHERE owner = @owner;


-- name: UpsertServiceEndpoint :one
INSERT INTO service_endpoints (
  id, entry, name, type, address
) VALUES (
  coalesce(sqlc.narg('id'), gen_random_uuid()),
  @entry,
  @name,
  @type,
  @address
)
ON CONFLICT (id) DO UPDATE SET
  entry = @entry,
  name = @name,
  type = @type,
  address = @address
WHERE service_endpoints.id = sqlc.narg('id')
RETURNING *;

-- name: DeleteOtherOwnedServiceEndpoints :exec
DELETE FROM service_endpoints WHERE entry = @entry AND ID <> ALL(@ids::uuid[]);

-- name: DeleteAllOwnedServiceEndpoints :exec
DELETE FROM service_endpoints WHERE entry = @entry;


-- name: UpsertAddress :one
INSERT INTO addresses (
  id, entry, type
) VALUES (
  coalesce(sqlc.narg('id'), gen_random_uuid()),
  @entry,
  @type
)
ON CONFLICT (id) DO UPDATE SET
  entry = @entry,
  type = @type
WHERE addresses.id = sqlc.narg('id')
RETURNING *;

-- name: DeleteOtherOwnedAddresses :exec
DELETE FROM addresses WHERE entry = @entry AND ID <> ALL(@ids::uuid[]);

-- name: DeleteAllOwnedAddresses :exec
DELETE FROM addresses WHERE entry = @entry;


-- name: CreateAddressComponent :one
INSERT INTO address_components (
  address, seq, type, value
) VALUES (
  @address,
  @seq,
  @type,
  @value
)
RETURNING *;

-- name: DeleteAllOwnedAddressComponents :exec
DELETE FROM address_components WHERE address = @address;

-- name: CreateTier :one
INSERT INTO tiers (
  name
) VALUES (
  @name
)
RETURNING *;

-- name: CreateNetwork :one
INSERT INTO networks (
  name
) VALUES (
  @name
)
RETURNING *;

-- name: CreateMembership :one
INSERT INTO memberships (
  institution
) VALUES (
  @institution
)
RETURNING *;

-- name: CreateNetworkMembership :one
INSERT INTO membership_networks (
  membership, network
) VALUES (
  @membership,
  @network
)
RETURNING *;

-- name: CreateTierMembership :one
INSERT INTO membership_tiers (
  membership, tier
) VALUES (
  @membership,
  @tier
)
RETURNING *;

-- name: CreateClosure :one
INSERT INTO closures (
  entry, start_date, end_date, reason
) VALUES (
  @entry,
  @start_date,
  @end_date,
  @reason
)
RETURNING *;

-- name: UpsertLMSConfig :one
INSERT INTO  lms_configs (
  id, entry, address, from_agency, from_agency_authentication, to_agency, lookup_user_enabled,
  accept_item_enabled, checkin_item_enabled, checkout_item_enabled, item_location, 
  request_item_request_type, request_item_scope_type, request_item_bib_code,
  request_item_pickup_location_enabled, requester_pickup_location, supplier_pickup_location,
  requester_patron_pattern
) VALUES (
  coalesce(sqlc.narg('id'), gen_random_uuid()),
  @entry,
  @address,
  @from_agency,
  @from_agency_authentication,
  @to_agency,
  @lookup_user_enabled,
  @accept_item_enabled,
  @checkin_item_enabled,
  @checkout_item_enabled,
  @item_location,
  @request_item_request_type,
  @request_item_scope_type,
  @request_item_bib_code,
  @request_item_pickup_location_enabled,
  @requester_pickup_location,
  @supplier_pickup_location,
  @requester_patron_pattern
)
ON CONFLICT (entry) DO UPDATE SET
  address = @address,
  from_agency = @from_agency,
  from_agency_authentication = @from_agency_authentication,
  to_agency = @to_agency,
  lookup_user_enabled = @lookup_user_enabled,
  accept_item_enabled = @accept_item_enabled,
  checkin_item_enabled = @checkin_item_enabled,
  checkout_item_enabled = @checkout_item_enabled,
  item_location = @item_location,
  request_item_request_type = @request_item_request_type,
  request_item_scope_type = @request_item_scope_type,
  request_item_bib_code = @request_item_bib_code,
  request_item_pickup_location_enabled = @request_item_pickup_location_enabled,
  requester_pickup_location = @requester_pickup_location,
  supplier_pickup_location = @supplier_pickup_location,
  requester_patron_pattern = @requester_patron_pattern
WHERE lms_configs.entry = sqlc.narg('entry')
RETURNING *;

-- name: GetNetworkById :one
SELECT * FROM networks WHERE id = $1 LIMIT 1;

-- name: GetTierById :one
SELECT * FROM tiers WHERE id = $1 LIMIT 1;

-- name: GetMembershipById :one
SELECT * FROM memberships WHERE id = $1 LIMIT 1;

-- name: GetNetworkMembershipById :one
SELECT * FROM membership_networks WHERE id = $1 LIMIT 1;

-- name: GetTierMembershipById :one
SELECT * FROM membership_tiers WHERE id = $1 LIMIT 1;

-- name: GetClosureById :one
SELECT * FROM closures WHERE id = $1 LIMIT 1;

-- name: GetClosureByIdForUpdate :one
SELECT * FROM closures WHERE id = $1 LIMIT 1 FOR UPDATE;

-- name: DeleteNetworkById :exec
DELETE from networks where id = @id;

-- name: DeleteTierById :exec
DELETE from tiers where id = @id;

-- name: DeleteNetworkMembershipById :exec
DELETE from membership_networks WHERE id = @id;

-- name: DeleteTierMembershipById :exec
DELETE from membership_tiers WHERE id = @id;

-- name: DeleteClosureById :exec
DELETE from closures where id = @id;

-- name: DeleteMembershipById :exec
DELETE from memberships where id = @id;

-- name: UpdateClosure :exec
UPDATE closures
SET
  start_date = @start_date,
  end_date = @end_date,
  reason = @reason
WHERE id = @id;

-- name: ListNetworks :many
SELECT * FROM networks
  LIMIT sqlc.arg('limit')
  OFFSET sqlc.arg('offset');

-- name: ListTiers :many
SELECT * FROM tiers
  LIMIT sqlc.arg('limit')
  OFFSET sqlc.arg('offset');

-- name: ListClosures :many
SELECT * FROM closures
  LIMIT sqlc.arg('limit')
  OFFSET sqlc.arg('offset');

-- name: ListMemberships :many
SELECT * FROM memberships
  LIMIT sqlc.arg('limit')
  OFFSET sqlc.arg('offset');

-- name: ListNetworkMemberships :many
SELECT * FROM membership_networks
  LIMIT sqlc.arg('limit')
  OFFSET sqlc.arg('offset');

-- name: ListTierMemberships :many
SELECT * FROM membership_tiers
  LIMIT sqlc.arg('limit')
  OFFSET sqlc.arg('offset');


-- name: GetLMSConfigByEntry :one
SELECT * FROM lms_configs 
  WHERE entry = @entry;