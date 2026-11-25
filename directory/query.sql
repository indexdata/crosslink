-- name: EntryById :one
SELECT * FROM entries WHERE id = $1 LIMIT 1;

-- name: EntryByIdForUpdate :one
SELECT * FROM entries WHERE id = $1 LIMIT 1 FOR UPDATE;

-- name: EntryByIdForImportUpdate :one
SELECT * FROM entries WHERE id = $1 LIMIT 1 FOR UPDATE NOWAIT;

-- name: LockEntryImportID :exec
SELECT pg_advisory_xact_lock(hashtextextended('directoryish:entry-id:' || (@id::uuid)::text, 0));

-- name: EntryBySymbolForUpdate :one
SELECT e.* FROM entries e, symbols s WHERE e.id = s.owner AND s.authority = @authority AND s.symbol = @symbol LIMIT 1 FOR UPDATE OF e;

-- name: EntryBySymbol :one
SELECT e.* FROM entries e, symbols s WHERE e.id = s.owner AND s.authority = @authority AND s.symbol = @symbol LIMIT 1;

-- name: SymbolByAuthorityAndSymbolForUpdate :one
SELECT * FROM symbols WHERE authority = @authority AND symbol = @symbol LIMIT 1 FOR UPDATE;

-- name: FirstSymbolByOwner :one
SELECT * FROM symbols WHERE owner = @owner AND position(':' in authority) = 0 ORDER BY authority, symbol LIMIT 1;

-- name: GetConsortialEntry :one
SELECT * FROM entries WHERE type = 'Consortium' LIMIT 1;

-- name: EntriesByParent :many
SELECT * FROM entries WHERE parent = @parent;

-- name: EntriesByIds :many
SELECT * FROM entries WHERE id = ANY(@ids::uuid[]);

-- name: LockConsortiumEntryChanges :exec
SELECT pg_advisory_xact_lock(hashtextextended('directoryish:consortium-entry', 0));

-- name: LockEntryImportKey :exec
SELECT pg_advisory_xact_lock(hashtextextended(
  'directoryish:entry:' || octet_length(@authority::text)::text || ':' || @authority::text ||
  ':' || octet_length(@symbol::text)::text || ':' || @symbol::text,
  0
));

-- name: CreateEntry :one
INSERT INTO entries (
  name, description, contact_name, email, from_email, tenant, vendor, phone_number, time_zone, organization_id, type, parent, lms_location_code, lend_to_borrow_ratio, hrid
) VALUES (
  $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15
)
RETURNING *;

-- name: CreateImportedEntry :one
INSERT INTO entries (
  id, name, description, contact_name, email, from_email, tenant, vendor, phone_number, time_zone, organization_id, type, parent, lms_location_code, lend_to_borrow_ratio, hrid
) VALUES (
  $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16
)
RETURNING *;

-- name: UpdateEntry :exec
UPDATE entries
SET
  name = @name,
  description = @description,
  contact_name = @contact_name,
  email = @email,
  from_email = @from_email,
  tenant = @tenant,
  vendor = @vendor,
  phone_number = @phone_number,
  time_zone = @time_zone,
  organization_id = @organization_id,
  type = @type,
  parent = @parent,
  lms_location_code = @lms_location_code,
  lend_to_borrow_ratio = @lend_to_borrow_ratio,
  hrid = @hrid

WHERE id = @id;

-- name: DeleteEntryById :exec
DELETE from entries WHERE id = @id;

-- name: DeleteEntryBySymbol :exec
DELETE from entries USING symbols
WHERE entries.id = symbols.owner
  AND symbols.authority = @authority
  AND symbols.symbol = @symbol;

-- name: CreateSymbol :one
INSERT INTO symbols (
  owner, symbol, authority
) VALUES (
  @owner,
  @symbol,
  @authority
)
RETURNING *;

-- name: UpdateOwnedSymbol :one
UPDATE symbols SET
  symbol = @symbol,
  authority = @authority
WHERE id = @id AND owner = @owner
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
  name, consortium, level, type, cost
) VALUES (
  @name, @consortium, @level, @type, @cost
)
RETURNING *;

-- name: CreateNetwork :one
INSERT INTO networks (
  name, consortium, reciprocal
) VALUES (
  @name, @consortium, @reciprocal
)
RETURNING *;


-- name: CreateEntryNetwork :one
INSERT INTO entry_networks (
  entry, network, priority
) VALUES (
  @entry,
  @network,
  @priority
)
RETURNING *;

-- name: UpdateEntryConfigs :exec
-- Replace the entire local configuration. Callers must include every
-- container the entry should keep.
UPDATE entries SET configs = @configs WHERE id = @id;

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

-- name: GetNetworkById :one
SELECT * FROM networks WHERE id = $1 LIMIT 1;

-- name: GetNetworkByIdForUpdate :one
SELECT * FROM networks WHERE id = $1 LIMIT 1 FOR UPDATE;

-- name: GetTierById :one
SELECT * FROM tiers WHERE id = $1 LIMIT 1;

-- name: GetTierByIdForUpdate :one
SELECT * FROM tiers WHERE id = $1 LIMIT 1 FOR UPDATE;

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

-- name: UpdateNetwork :exec
UPDATE networks
SET reciprocal = @reciprocal
WHERE id = @id;

-- name: UpdateTier :exec
UPDATE tiers
SET level = @level,
    type = @type,
    cost = @cost
WHERE id = @id;

-- name: UpdateEntryNetwork :execrows
UPDATE entry_networks
SET priority = @priority
WHERE id = @id;

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
SELECT n.*, en.priority FROM networks n
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

-- name: LockTierByBusinessKey :one
SELECT * FROM tiers
WHERE consortium = @consortium AND name = @name
FOR UPDATE;

-- name: UpdateImportedTier :exec
UPDATE tiers
SET level = @level, type = @type, cost = @cost
WHERE id = @id;

-- name: DeleteEntryTiersByTier :exec
DELETE FROM entry_tiers WHERE tier = @tier;

-- name: LockNetworkByBusinessKey :one
SELECT * FROM networks
WHERE consortium = @consortium AND name = @name
FOR UPDATE;

-- name: UpdateImportedNetwork :exec
UPDATE networks
SET reciprocal = @reciprocal
WHERE id = @id;

-- name: DeleteEntryNetworksByNetwork :exec
DELETE FROM entry_networks WHERE network = @network;

-- name: UnexpiredClosuresByEntries :many
-- Closure dates have no time zone. Keep those that ended yesterday by the UTC
-- date, since they can still be in effect in zones behind UTC. The cutoff uses
-- UTC so that it does not depend on the session's TimeZone setting.
SELECT * FROM closures
WHERE entry = ANY(@ids::uuid[]) AND end_date >= (now() AT TIME ZONE 'UTC')::date - 1
ORDER BY entry, id;

-- name: DeleteClosuresByEntry :exec
DELETE FROM closures WHERE entry = @entry;

-- name: WouldCreateEntryCycle :one
WITH RECURSIVE descendants AS (
  SELECT id FROM entries WHERE parent = @child
  UNION ALL
  SELECT e.id FROM entries e JOIN descendants d ON e.parent = d.id
)
SELECT @parent::uuid = @child::uuid OR EXISTS (SELECT 1 FROM descendants WHERE id = @parent);
