-- name: EntryById :one
SELECT * FROM entries WHERE id = $1 LIMIT 1;

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
  name, consortium
) VALUES (
  @name, @consortium
)
RETURNING *;

-- name: CreateNetwork :one
INSERT INTO networks (
  name, consortium
) VALUES (
  @name, @consortium
)
RETURNING *;


-- name: CreateEntryNetwork :one
INSERT INTO entry_networks (
  entry, network
) VALUES (
  @entry,
  @network
)
RETURNING *;

-- name: CreateEntryTier :one
INSERT INTO entry_tiers (
  entry, tier
) VALUES (
  @entry,
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

-- name: GetEntryNetworkById :one
SELECT * FROM entry_networks WHERE id = $1 LIMIT 1;

-- name: GetEntryNetworkByNetworkAndEntry :one
SELECT * FROM entry_networks WHERE network = $1 AND entry = $2 LIMIT 1;

-- name: GetEntryTierById :one
SELECT * FROM entry_tiers WHERE id = $1 LIMIT 1;

-- name: GetEntryTierByTierAndEntry :one
SELECT * FROM entry_tiers WHERE tier = $1 AND entry = $2 LIMIT 1;

-- name: GetClosureById :one
SELECT * FROM closures WHERE id = $1 LIMIT 1;

-- name: GetClosureByIdForUpdate :one
SELECT * FROM closures WHERE id = $1 LIMIT 1 FOR UPDATE;

-- name: DeleteNetworkById :exec
DELETE from networks where id = @id;

-- name: DeleteTierById :exec
DELETE from tiers where id = @id;

-- name: DeleteEntryNetworkById :exec
DELETE from entry_networks WHERE id = @id;

-- name: DeleteEntryTierById :exec
DELETE from entry_tiers WHERE id = @id;

-- name: DeleteClosureById :exec
DELETE from closures where id = @id;

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

-- name: ListNetworksForEntry :many
SELECT * FROM networks n
JOIN entry_networks en ON n.id = en.network
WHERE en.entry = $1;

-- name: ListNetworksForConsortium :many
SELECT * FROM networks
WHERE consortium = $1;

-- name: ListTiers :many
SELECT * FROM tiers
  LIMIT sqlc.arg('limit')
  OFFSET sqlc.arg('offset');

-- name: ListTiersForEntry :many
SELECT * FROM tiers t
JOIN entry_tiers et ON t.id = et.tier
WHERE et.entry = $1;

-- name: ListTiersForConsortium :many
SELECT * FROM tiers
WHERE consortium = $1;

-- name: ListClosures :many
SELECT * FROM closures
  LIMIT sqlc.arg('limit')
  OFFSET sqlc.arg('offset');

-- name: ListEntryNetworks :many
SELECT * FROM entry_networks
  WHERE (
    sqlc.narg('field')::text IS NULL
    OR sqlc.narg('value')::text IS NULL
    OR CASE sqlc.narg('field')::text
      WHEN 'id' THEN id::text
      WHEN 'entry' THEN entry::text
      WHEN 'network' THEN network::text
    END = sqlc.narg('value')::text
  )
  LIMIT sqlc.arg('limit')
  OFFSET sqlc.arg('offset');

-- name: ListEntryTiers :many
SELECT * FROM entry_tiers
  WHERE (
    sqlc.narg('field')::text IS NULL
    OR sqlc.narg('value')::text IS NULL
    OR CASE sqlc.narg('field')::text
      WHEN 'id' THEN id::text
      WHEN 'entry' THEN entry::text
      WHEN 'tier' THEN tier::text
    END = sqlc.narg('value')::text
  )
  LIMIT sqlc.arg('limit')
  OFFSET sqlc.arg('offset');

-- name: GetLMSConfigByEntry :one
SELECT * FROM lms_configs 
  WHERE entry = @entry;
