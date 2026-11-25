package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"strings"

	"github.com/google/uuid"
	"github.com/indexdata/cql-go/cql"
	"github.com/indexdata/cql-go/pgcql"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/oapi-codegen/nullable"

	"github.com/indexdata/crosslink/directory/auth"
	"github.com/indexdata/crosslink/directory/db"
	"github.com/indexdata/crosslink/directory/domain"
	"github.com/indexdata/crosslink/directory/storedconfig"
)

func isOwnedEntry(authData *auth.AuthData, tenant *string) bool {
	if authData == nil || tenant == nil || *tenant == "" {
		return false
	}

	authTenant := authData.GetInstitution()
	return authTenant != "" && authTenant == *tenant
}

func canSeeSensitiveEntry(authData *auth.AuthData, tenant *string) bool {
	return authData.HasRole(auth.ConsortialAdminRole) || authData.HasRole(auth.SystemUserRole) ||
		(authData.HasRole(auth.InstitutionalAdminRole) && isOwnedEntry(authData, tenant))
}

func maybeUpdateEntryVendor(cur *string, patch nullable.Nullable[EntryVendor]) *string {
	if !patch.IsSpecified() {
		return cur
	}
	if patch.IsNull() {
		return nil
	}
	value := string(patch.MustGet())
	return &value
}

// scanEntryRow scans one entry. It also returns the entry's stored
// configuration containers, keyed by API property name.
func scanEntryRow(rows pgx.Rows) (Entry, storedconfig.Configs, int, error) {
	var (
		id                uuid.UUID
		name              string
		description       *string
		contactName       *string
		organizationId    *string
		email             *string
		fromEmail         *string
		tenant            *string
		vendor            *string
		phoneNumber       *string
		lmsLocationCode   *string
		lendToBorrowRatio *string
		configs           storedconfig.Configs
		hrid              *string
		timeZone          *string
		entryType         *string
		parent            *uuid.UUID
		symbolsJSON       [][]byte
		endpointsJSON     [][]byte
		addressesJSON     [][]byte
		tiersJSON         [][]byte
		networksJSON      [][]byte
		closuresJSON      [][]byte
		totalCount        int
	)

	if err := rows.Scan(&id, &name, &description, &organizationId, &contactName, &email, &fromEmail, &tenant, &vendor, &phoneNumber,
		&lmsLocationCode, &lendToBorrowRatio, &configs, &hrid, &timeZone, &entryType, &parent, &symbolsJSON, &endpointsJSON,
		&addressesJSON, &tiersJSON, &networksJSON, &closuresJSON, &totalCount); err != nil {
		return Entry{}, nil, 0, err
	}

	symbols, err := unmarshalJSONArray[Symbol](symbolsJSON)
	if err != nil {
		return Entry{}, nil, 0, fmt.Errorf("unmarshalling symbols: %w", err)
	}

	endpoints, err := unmarshalJSONArray[ServiceEndpoint](endpointsJSON)
	if err != nil {
		return Entry{}, nil, 0, fmt.Errorf("unmarshalling endpoints: %w", err)
	}

	addresses, err := unmarshalJSONArray[Address](addressesJSON)
	if err != nil {
		return Entry{}, nil, 0, fmt.Errorf("unmarshalling addresses: %w", err)
	}

	closures, err := unmarshalJSONArray[Closure](closuresJSON)
	if err != nil {
		return Entry{}, nil, 0, fmt.Errorf("unmarshalling closures: %w", err)
	}

	tiers, err := unmarshalJSONArray[Tier](tiersJSON)
	if err != nil {
		return Entry{}, nil, 0, fmt.Errorf("unmarshalling tiers: %w", err)
	}

	networks, err := unmarshalJSONArray[EntryNetworkDetails](networksJSON)
	if err != nil {
		return Entry{}, nil, 0, fmt.Errorf("unmarshalling networks config: %w", err)
	}

	// Use nil for empty arrays so they're omitted in JSON (omitempty)
	var symbolsPtr *[]Symbol
	if len(symbols) > 0 {
		symbolsPtr = &symbols
	}

	var endpointsPtr *[]ServiceEndpoint
	if len(endpoints) > 0 {
		endpointsPtr = &endpoints
	}

	var addressesPtr *[]Address
	if len(addresses) > 0 {
		addressesPtr = &addresses
	}

	var closuresPtr *[]Closure
	if len(closures) > 0 {
		closuresPtr = &closures
	}

	var tiersPtr *[]Tier
	if len(tiers) > 0 {
		tiersPtr = &tiers
	}

	var networksPtr *[]EntryNetworkDetails
	if len(networks) > 0 {
		networksPtr = &networks
	}

	typeValue := EntryType(*entryType)

	entry := Entry{
		Id:                &id,
		Name:              name,
		Type:              &typeValue,
		OrganizationId:    organizationId,
		Description:       description,
		ContactName:       contactName,
		Email:             email,
		FromEmail:         fromEmail,
		Tenant:            tenant,
		Hrid:              hrid,
		LmsLocationCode:   lmsLocationCode,
		LendToBorrowRatio: lendToBorrowRatio,
		PhoneNumber:       phoneNumber,
		Parent:            parent,
		Symbols:           symbolsPtr,
		Endpoints:         endpointsPtr,
		Addresses:         addressesPtr,
		Closures:          closuresPtr,
		Tiers:             tiersPtr,
		Networks:          networksPtr,
		TimeZone:          timeZone,
		Vendor:            (*EntryVendor)(vendor),
	}
	if err := applyConfigs(&entry, configs); err != nil {
		return Entry{}, nil, 0, err
	}
	return entry, configs, totalCount, nil
}

const defaultEntryOrder = "ORDER BY e.name, e.id"
const defaultEntryLimit = 10

// handleEntryCQL converts a CQL query string to a PostgreSQL WHERE clause
func handleEntryCQL(cqlString string, noBaseArgs int) (pgcql.Query, error) {
	def := pgcql.NewPgDefinition()

	def.AddField("cql.serverChoice", pgcql.NewFieldTsVector().
		WithLanguage("simple").WithServerChoiceRel(cql.ALL).WithColumn("e.search"))

	f := pgcql.NewFieldString().WithLikeOps().WithLower()
	f.SetColumn("e.name")
	def.AddField("name", f)

	f = pgcql.NewFieldString().WithLikeOps().WithLower()
	f.SetColumn("e.description")
	def.AddField("description", f)

	f = pgcql.NewFieldString().WithLikeOps()
	f.SetColumn("e.type")
	def.AddField("type", f)

	f = pgcql.NewFieldString().WithExact()
	f.SetColumn("e.parent")
	def.AddField("parent", f)

	f = pgcql.NewFieldString().WithExact()
	f.SetColumn("e.tenant")
	def.AddField("tenant", f)

	fb := pgcql.NewFieldBool()
	fb.SetColumn("COALESCE((e.configs->'illConfig'->>'isPickupLocation')::boolean, FALSE)")
	def.AddField("isPickupLocation", fb)

	def.AddField("symbol", &fieldEntrySymbol{index: "symbol", ownerColumn: "e.id"})
	def.AddField("parentSymbol", &fieldEntrySymbol{index: "parentSymbol", ownerColumn: "e.parent"})

	var parser cql.Parser
	query, err := parser.Parse(cqlString)
	if err != nil {
		return nil, err
	}
	return def.Parse(query, noBaseArgs+1)
}

type fieldEntrySymbol struct {
	index       string
	ownerColumn string
}

func (f *fieldEntrySymbol) GetColumn() string  { return "" }
func (f *fieldEntrySymbol) SetColumn(_ string) {}
func (f *fieldEntrySymbol) Sort() string       { return "" }
func (f *fieldEntrySymbol) Generate(sc cql.SearchClause, queryArgumentIndex int) (string, []any, error) {
	switch sc.Relation {
	case cql.EQ, cql.EXACT, "==":
		return fmt.Sprintf(`EXISTS (
			SELECT 1 FROM symbols entry_symbol
			WHERE entry_symbol.owner = %s
				AND (entry_symbol.authority || ':' || entry_symbol.symbol = $%d OR entry_symbol.symbol = $%d)
		)`, f.ownerColumn, queryArgumentIndex, queryArgumentIndex), []any{strings.ToUpper(sc.Term)}, nil
	case cql.ANY:
		terms := strings.Fields(strings.ToUpper(sc.Term))
		return fmt.Sprintf(`EXISTS (
			SELECT 1 FROM symbols entry_symbol
			WHERE entry_symbol.owner = %s
				AND (entry_symbol.authority || ':' || entry_symbol.symbol = ANY($%d::text[]) OR entry_symbol.symbol = ANY($%d::text[]))
		)`, f.ownerColumn, queryArgumentIndex, queryArgumentIndex), []any{terms}, nil
	default:
		return "", nil, fmt.Errorf("unsupported relation %s for %s", sc.Relation, f.index)
	}
}

// buildEntrySQL builds the base SQL query for entries with nested subresources
func buildEntrySQL(whereClause string) string {
	baseQuery := `

	SELECT
		e.id,
		e.name,
		e.description,
		e.organization_id,
		e.contact_name,
		e.email,
		e.from_email,
		e.tenant,
		e.vendor,
		e.phone_number,
		e.lms_location_code,
		e.lend_to_borrow_ratio,
		e.configs,
		e.hrid,
		e.time_zone,
		e.type,
		e.parent,
		ARRAY(SELECT row_to_json(s) FROM symbols s WHERE s.owner = e.id ORDER BY s.id) as symbols,
		ARRAY(SELECT row_to_json(ep) FROM service_endpoints ep WHERE ep.entry = e.id ORDER BY ep.id) as endpoints,
		ARRAY(
			SELECT row_to_json(a_with_components)
			FROM (
				SELECT
				a.id,
				a.type,
				ARRAY(
					SELECT row_to_json(ac)
					FROM address_components ac
						WHERE ac.address = a.id
						ORDER BY ac.seq
				) as "addressComponents"
			FROM addresses a
			WHERE a.entry = e.id
			ORDER BY a.id
			) a_with_components
		) as addresses,
		ARRAY(
			SELECT json_build_object(
				'id', tiers.id,
				'consortium', tiers.consortium,
				'name', tiers.name,
				'level', tiers.level,
				'type', tiers.type,
				'cost', tiers.cost
			) from entry_tiers INNER JOIN tiers ON tiers.id = entry_tiers.tier
			WHERE entry_tiers.entry = e.id
			ORDER BY tiers.id
		) as tiers,
		ARRAY(
			SELECT json_build_object(
				'id', networks.id,
				'consortium', networks.consortium,
				'name', networks.name,
				'priority', entry_networks.priority,
				'reciprocal', networks.reciprocal
			) from entry_networks INNER JOIN networks ON networks.id = entry_networks.network
			WHERE entry_networks.entry = e.id
			ORDER BY networks.id
		) as networks,
		ARRAY(
		SELECT json_build_object(
		'id', c.id,
		'entry', c.entry,
		'startDate', c.start_date::date,
		'endDate', c.end_date::date,
		'reason', c.reason
		)
		FROM closures c
		WHERE c.entry = e.id
		ORDER BY c.start_date, c.id
		) as closures,
		COUNT(*) OVER() as total_count
		FROM entries e
	`
	if whereClause != "" {
		return baseQuery + "\n" + whereClause
	}
	return baseQuery
}

func buildEntryListQuery(cqlString *string, limit *Limit, offset *Offset, baseWhere string, baseArgs []any) (string, []any, error) {
	whereClause := baseWhere
	args := baseArgs

	if cqlString != nil && *cqlString != "" {
		res, err := handleEntryCQL(*cqlString, len(args))
		if err != nil {
			return "", nil, err
		}

		if cqlWhere := res.GetWhereClause(); cqlWhere != "" {
			if whereClause == "" {
				whereClause = cqlWhere
			} else {
				// Parenthesize the complete CQL predicate so an OR expression cannot
				// broaden a caller-supplied base scope such as tenant ownership.
				whereClause += " AND (" + cqlWhere + ")"
			}
		}
		args = append(args, res.GetQueryArguments()...)
	}

	if whereClause != "" {
		whereClause = "WHERE " + whereClause + "\n"
	}
	query := buildEntrySQL(whereClause + defaultEntryOrder)

	entryLimit := defaultEntryLimit
	if limit != nil {
		entryLimit = int(*limit)
	}
	args = append(args, entryLimit)
	query += fmt.Sprintf("\nLIMIT $%d", len(args))

	if offset != nil {
		args = append(args, *offset)
		query += fmt.Sprintf("\nOFFSET $%d", len(args))
	}

	return query, args, nil
}

// readEntries runs an entry query, merges inherited configuration and
// closures when resolve is true, and sanitizes protected fields for the
// caller.
//
// When resolving, the entries and their ancestors are read in one read-only
// REPEATABLE READ transaction so that both come from the same snapshot.
func (a ApiImpl) readEntries(ctx context.Context, query string, args []any, authData *auth.AuthData, resolve bool) ([]Entry, int, error) {
	var q db.DBTX = a.pool
	resolve = resolve && a.inheritance != nil
	if resolve {
		tx, err := a.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
		if err != nil {
			return nil, 0, fmt.Errorf("begin entry read transaction: %w", err)
		}
		defer func() { _ = tx.Rollback(ctx) }()
		q = tx
	}

	rows, err := q.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("querying entries: %w", err)
	}
	// Initialise items as explicitly zero length because a nil slice is
	// JSON-encoded as null rather than [].
	items := make([]Entry, 0)
	var configs []storedconfig.Configs
	var totalCount int
	for rows.Next() {
		entry, stored, count, err := scanEntryRow(rows)
		if err != nil {
			rows.Close()
			return nil, 0, fmt.Errorf("scanning entry row: %w", err)
		}
		items = append(items, entry)
		configs = append(configs, stored)
		totalCount = count
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterating entry rows: %w", err)
	}

	if resolve {
		if err := a.resolveInheritance(ctx, db.New(q), authData, items, configs); err != nil {
			return nil, 0, err
		}
	}
	for i := range items {
		if !canSeeSensitiveEntry(authData, items[i].Tenant) {
			if err := sanitizeEntry(&items[i]); err != nil {
				return nil, 0, fmt.Errorf("sanitizing entry: %w", err)
			}
		}
	}
	return items, totalCount, nil
}

func (a ApiImpl) queryEntryList(ctx context.Context, query string, args []any, authData *auth.AuthData, resolve bool) (EntriesResponse, error) {
	items, totalCount, err := a.readEntries(ctx, query, args, authData, resolve)
	if err != nil {
		return EntriesResponse{}, err
	}
	return EntriesResponse{
		Items: items,
		About: About{Count: int64(totalCount)},
	}, nil
}

func (a ApiImpl) GetEntries(ctx context.Context, request GetEntriesRequestObject) (GetEntriesResponseObject, error) {

	authData := auth.GetAuthData(ctx)
	validRoles := []auth.DirectoryRole{auth.ConsortialAdminRole, auth.InstitutionalAdminRole, auth.SystemUserRole, auth.PublicUserRole}
	if !authData.HasRoleFromList(validRoles) {
		slog.DebugContext(ctx, "permission denied")
		return GetEntries403JSONResponse{forbidden("Access denied")}, nil
	}

	query, args, err := buildEntryListQuery(request.Params.Cql, request.Params.Limit, request.Params.Offset, "", nil)
	if err != nil {
		return GetEntries400JSONResponse{badRequest(fmt.Sprintf("CQL parse error: %v", err))}, nil
	}

	response, err := a.queryEntryList(ctx, query, args, authData, resolveInheritParam(request.Params.ResolveInherit))
	if err != nil {
		return nil, fmt.Errorf("failed to read entries: %w", err)
	}

	return GetEntries200JSONResponse(response), nil
}

func (a ApiImpl) GetOwnedEntries(ctx context.Context, request GetOwnedEntriesRequestObject) (GetOwnedEntriesResponseObject, error) {
	authData := auth.GetAuthData(ctx)
	validRoles := []auth.DirectoryRole{auth.ConsortialAdminRole, auth.InstitutionalAdminRole, auth.SystemUserRole, auth.PublicUserRole}
	if !authData.HasRoleFromList(validRoles) {
		slog.DebugContext(ctx, "permission denied")
		return GetOwnedEntries403JSONResponse{forbidden("Access denied")}, nil
	}

	tenant := authData.GetInstitution()
	if tenant == "" {
		return GetOwnedEntries400JSONResponse{badRequest("Tenant is required")}, nil
	}

	query, args, err := buildEntryListQuery(
		request.Params.Cql,
		request.Params.Limit,
		request.Params.Offset,
		"e.tenant = $1",
		[]any{tenant},
	)
	if err != nil {
		return GetOwnedEntries400JSONResponse{badRequest(fmt.Sprintf("CQL parse error: %v", err))}, nil
	}

	response, err := a.queryEntryList(ctx, query, args, authData, resolveInheritParam(request.Params.ResolveInherit))
	if err != nil {
		return nil, fmt.Errorf("failed to read owned entries: %w", err)
	}

	return GetOwnedEntries200JSONResponse(response), nil
}

func (a ApiImpl) GetEntry(ctx context.Context, request GetEntryRequestObject) (GetEntryResponseObject, error) {
	var query string
	var args []interface{}

	authData := auth.GetAuthData(ctx)

	validRoles := []auth.DirectoryRole{auth.ConsortialAdminRole, auth.InstitutionalAdminRole, auth.SystemUserRole, auth.PublicUserRole}
	if !authData.HasRoleFromList(validRoles) {
		slog.DebugContext(ctx, "permission denied")
		return GetEntry403JSONResponse{forbidden("Access denied")}, nil
	}

	switch request.Key {
	case GetEntryParamsKeyById:
		parsedId, perr := uuid.Parse(request.Value)
		if perr != nil {
			return GetEntry400JSONResponse{badRequest(invalidIDMessage)}, nil
		}
		query = buildEntrySQL("WHERE e.id = $1")
		args = []interface{}{parsedId}
	case GetEntryParamsKeyBySymbol:
		authority, symbol, perr := resolveCombinedSymbol(request.Value)
		if perr != nil {
			return GetEntry400JSONResponse{badRequest(invalidSymbolMessage)}, nil
		}
		query = buildEntrySQL(`
			WHERE e.id = (
				SELECT owner FROM symbols WHERE authority = $1 AND symbol = $2
			)
		`)
		args = []interface{}{authority, symbol}
	}

	entries, _, err := a.readEntries(ctx, query, args, authData, resolveInheritParam(request.Params.ResolveInherit))
	if err != nil {
		return nil, fmt.Errorf("failed to read entry: %w", err)
	}
	if len(entries) == 0 {
		return GetEntry404JSONResponse{notFound("Entry not found")}, nil
	}

	return GetEntry200JSONResponse(entries[0]), nil
}

func sanitizeEntry(entry *Entry) error {
	return Sanitize(entry)

}

func validateLendersOfLastResort(ctx context.Context, queries *db.Queries, lenders *[]SymbolProperties) (string, error) {
	if lenders == nil {
		return "", nil
	}

	type symbolKey struct {
		authority string
		symbol    string
	}

	seen := make(map[symbolKey]struct{}, len(*lenders))
	keys := make([]symbolKey, 0, len(*lenders))
	for _, lender := range *lenders {
		key := symbolKey{
			authority: strings.ToUpper(lender.Authority),
			symbol:    strings.ToUpper(lender.Symbol),
		}
		if strings.Contains(key.authority, ":") {
			return "Lender of last resort authority must not contain ':'", nil
		}
		if _, exists := seen[key]; exists {
			return fmt.Sprintf("Duplicate lender of last resort: %s:%s", key.authority, key.symbol), nil
		}
		seen[key] = struct{}{}
		keys = append(keys, key)
	}

	for _, key := range keys {
		_, err := queries.EntryBySymbol(ctx, db.EntryBySymbolParams{Authority: key.authority, Symbol: key.symbol})
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Sprintf("Lender of last resort does not exist: %s:%s", key.authority, key.symbol), nil
		}
		if err != nil {
			return "", fmt.Errorf("lookup lender of last resort %s:%s: %w", key.authority, key.symbol, err)
		}
	}

	return "", nil
}

func (a ApiImpl) AddEntry(ctx context.Context, request AddEntryRequestObject) (AddEntryResponseObject, error) {

	authData := auth.GetAuthData(ctx)
	validRoles := []auth.DirectoryRole{auth.ConsortialAdminRole}
	if !authData.HasRoleFromList(validRoles) {
		slog.DebugContext(ctx, "permission denied")
		return AddEntry403JSONResponse{forbidden("Access denied")}, nil
	}

	tx, err := a.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	qtx := a.queries.WithTx(tx)
	entryType := derefOrDefault(request.Body.Type, EntryType("Institution"))

	if entryType == "Consortium" {
		if err := qtx.LockConsortiumEntryChanges(ctx); err != nil {
			return nil, fmt.Errorf("failed to lock consortium entry changes: %w", err)
		}
		_, err := qtx.GetConsortialEntry(ctx)
		if err == nil {
			return AddEntry400JSONResponse{badRequest("An entry of type Consortium already exists")}, nil
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("failed to check for consortium entry: %w", err)
		}
	}

	if request.Body.Parent != nil {
		parentEntry, err := qtx.EntryByIdForUpdate(ctx, *request.Body.Parent)
		if errors.Is(err, pgx.ErrNoRows) {
			return AddEntry400JSONResponse{badRequest("Value for Parent is not a valid Entry")}, nil
		} else if err != nil {
			return nil, fmt.Errorf("failed to fetch parent entry: %w", err)
		}
		validParent, reason := domain.ValidParentForType(string(entryType), parentEntry.Type)
		if !validParent {
			return AddEntry400JSONResponse{badRequest("Invalid entry for parent: " + reason)}, nil
		}
	}

	supplied, err := bodyProperties(request.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read request body properties: %w", err)
	}
	violation, err := a.checkConfigWrite(string(entryType), supplied, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to check configuration editability: %w", err)
	}
	if violation != "" {
		return AddEntry400JSONResponse{badRequest(violation)}, nil
	}

	toInsert := db.CreateEntryParams{
		Name:              request.Body.Name,
		Description:       request.Body.Description,
		ContactName:       request.Body.ContactName,
		Email:             request.Body.Email,
		FromEmail:         request.Body.FromEmail,
		Tenant:            request.Body.Tenant,
		PhoneNumber:       request.Body.PhoneNumber,
		TimeZone:          request.Body.TimeZone,
		OrganizationID:    request.Body.OrganizationId,
		Type:              string(entryType),
		Parent:            request.Body.Parent,
		LmsLocationCode:   request.Body.LmsLocationCode,
		LendToBorrowRatio: request.Body.LendToBorrowRatio,
		Hrid:              request.Body.Hrid,
	}
	if request.Body.Vendor != nil {
		vendor := string(*request.Body.Vendor)
		toInsert.Vendor = &vendor
	}
	insertedEntry, err := qtx.CreateEntry(ctx, toInsert)
	if isUniqueConstraintViolation(err, "entries_hrid_key") {
		return AddEntry409JSONResponse{Error: "An entry with this hrid already exists"}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to create entry name=%v: %w", request.Body.Name, err)
	}

	if request.Body.Symbols != nil {
		for _, symbol := range *request.Body.Symbols {
			_, err = qtx.CreateSymbol(ctx, db.CreateSymbolParams{
				Owner:     insertedEntry.ID,
				Symbol:    strings.ToUpper(symbol.Symbol),
				Authority: strings.ToUpper(symbol.Authority),
			})
			if err != nil {
				var pge *pgconn.PgError
				if errors.As(err, &pge) {
					if pge.SQLState() == "23505" { //unique_violation
						slog.InfoContext(ctx, "duplicate symbol rejected", "symbol", symbol.Symbol, "authority", symbol.Authority)
						return AddEntry400JSONResponse{badRequest("Duplicate symbol")}, nil
					}
				}
				return nil, fmt.Errorf("failed to create symbol symbol=%v authority=%v: %w", symbol.Symbol, symbol.Authority, err)
			}
		}
	}

	if request.Body.Endpoints != nil {
		for _, endpoint := range *request.Body.Endpoints {
			_, err = qtx.UpsertServiceEndpoint(ctx, db.UpsertServiceEndpointParams{
				Entry:   insertedEntry.ID,
				Name:    endpoint.Name,
				Type:    endpoint.Type,
				Address: endpoint.Address,
			})
			if err != nil {
				return nil, fmt.Errorf("failed to create service endpoint name=%v type=%v: %w", endpoint.Name, endpoint.Type, err)
			}
		}
	}

	if request.Body.Addresses != nil {
		for _, address := range *request.Body.Addresses {
			insertedAddress, err := qtx.UpsertAddress(ctx, db.UpsertAddressParams{
				Entry: insertedEntry.ID,
				Type:  string(address.Type),
			})
			if err != nil {
				return nil, fmt.Errorf("failed to upsert address type=%v: %w", address.Type, err)
			}

			if address.AddressComponents != nil {
				for _, component := range *address.AddressComponents {
					_, err = qtx.CreateAddressComponent(ctx, db.CreateAddressComponentParams{
						Address: insertedAddress.ID,
						Seq:     component.Seq,
						Type:    string(component.Type),
						Value:   component.Value,
					})
					if err != nil {
						return nil, fmt.Errorf("failed to create address component type=%v seq=%v: %w", component.Type, component.Seq, err)
					}
				}
			}
		}
	}

	if request.Body.IllConfig != nil {
		validationMessage, validationErr := validateLendersOfLastResort(ctx, qtx, request.Body.IllConfig.LendersOfLastResort)
		if validationErr != nil {
			return nil, fmt.Errorf("failed to validate lenders of last resort: %w", validationErr)
		}
		if validationMessage != "" {
			return AddEntry400JSONResponse{badRequest(validationMessage)}, nil
		}
	}
	configs := storedconfig.Configs{}
	_, message, err := writeContainers(configs, supplied, true)
	if err != nil {
		return nil, fmt.Errorf("failed to encode configuration: %w", err)
	}
	if message != "" {
		return AddEntry400JSONResponse{badRequest(message)}, nil
	}
	if len(configs) > 0 {
		if err := qtx.UpdateEntryConfigs(ctx, db.UpdateEntryConfigsParams{ID: insertedEntry.ID, Configs: configs}); err != nil {
			return nil, fmt.Errorf("failed to store configuration: %w", err)
		}
	}

	var resp Id
	resp.Id = insertedEntry.ID

	err = tx.Commit(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	return AddEntry201JSONResponse(resp), nil
}

func (a ApiImpl) UpdateEntry(ctx context.Context, request UpdateEntryRequestObject) (UpdateEntryResponseObject, error) {

	authData := auth.GetAuthData(ctx)
	validRoles := []auth.DirectoryRole{auth.ConsortialAdminRole, auth.InstitutionalAdminRole}
	writeRoles := []auth.DirectoryRole{auth.ConsortialAdminRole}

	if !authData.HasRoleFromList(validRoles) {
		slog.DebugContext(ctx, "permission denied")
		return UpdateEntry403JSONResponse{forbidden("Access denied")}, nil
	}

	tx, err := a.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	qtx := a.queries.WithTx(tx)

	var orig db.Entry
	switch request.Key {
	case UpdateEntryParamsKeyById:
		parsedId, perr := uuid.Parse(request.Value)
		if perr != nil {
			return UpdateEntry400JSONResponse{badRequest(invalidIDMessage)}, nil
		}
		orig, err = qtx.EntryByIdForUpdate(ctx, parsedId)
	case UpdateEntryParamsKeyBySymbol:
		authority, symbol, perr := resolveCombinedSymbol(request.Value)
		if perr != nil {
			return UpdateEntry400JSONResponse{badRequest(invalidSymbolMessage)}, nil
		}
		orig, err = qtx.EntryBySymbolForUpdate(ctx, db.EntryBySymbolForUpdateParams{Authority: authority, Symbol: symbol})
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return UpdateEntry404JSONResponse{notFound("Entry not found")}, nil
	} else if err != nil {
		return nil, fmt.Errorf("failed to fetch entry for update: %w", err)
	}

	if !authData.HasRoleFromList(writeRoles) && (!isOwnedEntry(authData, orig.Tenant) || request.Body.Tenant.IsSpecified()) {
		slog.DebugContext(ctx, "permission denied")
		return UpdateEntry403JSONResponse{forbidden("Access denied")}, nil
	}

	if request.Body.Type.IsSpecified() && request.Body.Type.IsNull() {
		slog.ErrorContext(ctx, "type cannot be null")
		return UpdateEntry400JSONResponse{badRequest("'type' cannot be set to null")}, nil
	}
	origTypeEntryPatch := EntryType(orig.Type)
	resultingType := string(*maybeUpdateCol(&origTypeEntryPatch, request.Body.Type))
	var parentEntry db.Entry
	parent := maybeUpdateCol(orig.Parent, request.Body.Parent)
	// An entry's parent and type decide what it inherits, so only consortial
	// administrators may change them. Resending the stored values is allowed.
	parentChanged := (parent == nil) != (orig.Parent == nil) || parent != nil && *parent != *orig.Parent
	if (parentChanged || resultingType != orig.Type) && !authData.HasRoleFromList(writeRoles) {
		return UpdateEntry403JSONResponse{forbidden("Only consortial administrators may change an entry's parent or type")}, nil
	}
	if parent != nil {
		if *parent == orig.ID {
			return UpdateEntry400JSONResponse{badRequest("An entry cannot be its own parent")}, nil
		}
		parentEntry, err = qtx.EntryByIdForUpdate(ctx, *parent)
		if errors.Is(err, pgx.ErrNoRows) {
			return UpdateEntry400JSONResponse{badRequest("Value for Parent is not a valid Entry")}, nil
		} else if err != nil {
			return nil, fmt.Errorf("failed to fetch parent entry: %w", err)
		}
	}

	if parent != nil {
		validParent, reason := domain.ValidParentForType(resultingType, parentEntry.Type)
		if !validParent {
			return UpdateEntry400JSONResponse{badRequest("Invalid entry for parent: " + reason)}, nil
		}
	}

	if resultingType != orig.Type {
		children, err := qtx.EntriesByParent(ctx, &orig.ID)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch child entries: %w", err)
		}
		resultingParent := orig
		resultingParent.Type = resultingType
		for _, child := range children {
			valid, reason := domain.ValidParentForType(child.Type, resultingParent.Type)
			if !valid {
				return UpdateEntry400JSONResponse{badRequest("Entry type is invalid for existing child: " + reason)}, nil
			}
		}
	}

	supplied, err := bodyProperties(request.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read request body properties: %w", err)
	}
	violation, err := a.checkConfigWrite(resultingType, supplied, orig.Configs)
	if err != nil {
		return nil, fmt.Errorf("failed to check configuration editability: %w", err)
	}
	if violation != "" {
		return UpdateEntry400JSONResponse{badRequest(violation)}, nil
	}

	if resultingType == "Consortium" || orig.Type == "Consortium" {
		if err := qtx.LockConsortiumEntryChanges(ctx); err != nil {
			return nil, fmt.Errorf("failed to lock consortium entry changes: %w", err)
		}
	}
	if resultingType == "Consortium" && resultingType != orig.Type {
		consortialEntry, err := qtx.GetConsortialEntry(ctx)
		if err == nil && consortialEntry.ID != orig.ID {
			return UpdateEntry400JSONResponse{badRequest("An entry of type Consortium already exists")}, nil
		} else if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("failed to check for consortium entry: %w", err)
		}
	}

	err = qtx.UpdateEntry(ctx, db.UpdateEntryParams{
		Name:              derefOrDefault(request.Body.Name, orig.Name),
		Description:       maybeUpdateCol(orig.Description, request.Body.Description),
		ContactName:       maybeUpdateCol(orig.ContactName, request.Body.ContactName),
		Email:             maybeUpdateCol(orig.Email, request.Body.Email),
		FromEmail:         maybeUpdateCol(orig.FromEmail, request.Body.FromEmail),
		Tenant:            maybeUpdateCol(orig.Tenant, request.Body.Tenant),
		Vendor:            maybeUpdateEntryVendor(orig.Vendor, request.Body.Vendor),
		PhoneNumber:       maybeUpdateCol(orig.PhoneNumber, request.Body.PhoneNumber),
		Parent:            maybeUpdateCol(orig.Parent, request.Body.Parent),
		LmsLocationCode:   maybeUpdateCol(orig.LmsLocationCode, request.Body.LmsLocationCode),
		LendToBorrowRatio: maybeUpdateCol(orig.LendToBorrowRatio, request.Body.LendToBorrowRatio),
		Hrid:              maybeUpdateCol(orig.Hrid, request.Body.Hrid),
		Type:              resultingType,
		TimeZone:          maybeUpdateCol(orig.TimeZone, request.Body.TimeZone),
		OrganizationID:    maybeUpdateCol(orig.OrganizationID, request.Body.OrganizationId),
		ID:                orig.ID,
	})
	if isUniqueConstraintViolation(err, "entries_hrid_key") {
		return UpdateEntry409JSONResponse{Error: "An entry with this hrid already exists"}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to update entry id=%v: %w", orig.ID, err)
	}

	if request.Body.Symbols.IsSpecified() && !request.Body.Symbols.IsNull() {
		reqsyms := request.Body.Symbols.MustGet()
		// Delete existing symbols not present
		var patchedSymbols []uuid.UUID
		for _, symbol := range reqsyms {
			if symbol.Id != nil {
				patchedSymbols = append(patchedSymbols, *symbol.Id)
			}
		}
		if len(patchedSymbols) > 0 {
			err = qtx.DeleteOtherOwnedSymbols(ctx, db.DeleteOtherOwnedSymbolsParams{Owner: orig.ID, Ids: patchedSymbols})
			if err != nil {
				return nil, fmt.Errorf("failed to delete other owned symbols entry_id=%v: %w", orig.ID, err)
			}
		} else {
			err = qtx.DeleteAllOwnedSymbols(ctx, orig.ID)
			if err != nil {
				return nil, fmt.Errorf("failed to delete all owned symbols entry_id=%v: %w", orig.ID, err)
			}
		}

		// The entry is already locked. Restrict updates to its own symbols so
		// the search trigger never needs to lock another entry after a symbol.
		for _, symbol := range reqsyms {
			if symbol.Id == nil {
				_, err = qtx.CreateSymbol(ctx, db.CreateSymbolParams{
					Owner: orig.ID, Symbol: strings.ToUpper(symbol.Symbol), Authority: strings.ToUpper(symbol.Authority),
				})
			} else {
				_, err = qtx.UpdateOwnedSymbol(ctx, db.UpdateOwnedSymbolParams{
					ID: *symbol.Id, Owner: orig.ID, Symbol: strings.ToUpper(symbol.Symbol), Authority: strings.ToUpper(symbol.Authority),
				})
				if errors.Is(err, pgx.ErrNoRows) {
					return UpdateEntry400JSONResponse{badRequest("Symbol ID does not belong to this entry")}, nil
				}
			}
			if err != nil {
				var pge *pgconn.PgError
				if errors.As(err, &pge) {
					if pge.SQLState() == "23505" { //unique_violation
						slog.InfoContext(ctx, "duplicate symbol rejected", "symbol", symbol.Symbol, "authority", symbol.Authority)
						return UpdateEntry400JSONResponse{badRequest("Duplicate symbol")}, nil
					}
				}
				return nil, fmt.Errorf("unexpected database error during symbol upsert symbol=%v authority=%v: %w", symbol.Symbol, symbol.Authority, err)
			}
		}
	} else if request.Body.Symbols.IsNull() {
		err = qtx.DeleteAllOwnedSymbols(ctx, orig.ID)
		if err != nil {
			return nil, fmt.Errorf("failed to delete all owned symbols entry_id=%v: %w", orig.ID, err)
		}
	}

	if request.Body.Endpoints.IsSpecified() && !request.Body.Endpoints.IsNull() {
		reqeps := request.Body.Endpoints.MustGet()
		// Delete existing endpoints not present
		var patchedEndpoints []uuid.UUID
		for _, endpoint := range reqeps {
			if endpoint.Id != nil {
				patchedEndpoints = append(patchedEndpoints, *endpoint.Id)
			}
		}
		if len(patchedEndpoints) > 0 {
			err = qtx.DeleteOtherOwnedServiceEndpoints(ctx, db.DeleteOtherOwnedServiceEndpointsParams{Entry: orig.ID, Ids: patchedEndpoints})
			if err != nil {
				return nil, fmt.Errorf("failed to delete other owned service endpoints entry_id=%v: %w", orig.ID, err)
			}
		} else {
			err = qtx.DeleteAllOwnedServiceEndpoints(ctx, orig.ID)
			if err != nil {
				return nil, fmt.Errorf("failed to delete all owned service endpoints entry_id=%v: %w", orig.ID, err)
			}
		}

		// Update/create endpoints
		for _, endpoint := range reqeps {
			_, err = qtx.UpsertServiceEndpoint(ctx, db.UpsertServiceEndpointParams{
				ID:      endpoint.Id,
				Entry:   orig.ID,
				Name:    endpoint.Name,
				Type:    endpoint.Type,
				Address: endpoint.Address,
			})
			if err != nil {
				return nil, fmt.Errorf("failed to upsert service endpoint name=%v type=%v: %w", endpoint.Name, endpoint.Type, err)
			}
		}
	} else if request.Body.Endpoints.IsNull() {
		err = qtx.DeleteAllOwnedServiceEndpoints(ctx, orig.ID)
		if err != nil {
			return nil, fmt.Errorf("failed to delete all owned service endpoints entry_id=%v: %w", orig.ID, err)
		}
	}

	if request.Body.Addresses.IsSpecified() && !request.Body.Addresses.IsNull() {
		reqaddrs := request.Body.Addresses.MustGet()
		// Delete existing addresses not present
		var patchedAddresses []uuid.UUID
		for _, address := range reqaddrs {
			if address.Id != nil {
				patchedAddresses = append(patchedAddresses, *address.Id)
			}
		}
		if len(patchedAddresses) > 0 {
			err = qtx.DeleteOtherOwnedAddresses(ctx, db.DeleteOtherOwnedAddressesParams{Entry: orig.ID, Ids: patchedAddresses})
			if err != nil {
				return nil, fmt.Errorf("failed to delete other owned addresses entry_id=%v: %w", orig.ID, err)
			}
		} else {
			err = qtx.DeleteAllOwnedAddresses(ctx, orig.ID)
			if err != nil {
				return nil, fmt.Errorf("failed to delete all owned addresses entry_id=%v: %w", orig.ID, err)
			}
		}

		// Update/create addresses
		for _, address := range reqaddrs {
			insertedAddress, err := qtx.UpsertAddress(ctx, db.UpsertAddressParams{
				ID:    address.Id,
				Entry: orig.ID,
				Type:  string(address.Type),
			})
			if err != nil {
				return nil, fmt.Errorf("failed to upsert address type=%v: %w", address.Type, err)
			}

			// Handle address components
			if address.AddressComponents != nil {
				// Delete all existing components and insert new ones
				err = qtx.DeleteAllOwnedAddressComponents(ctx, insertedAddress.ID)
				if err != nil {
					return nil, fmt.Errorf("failed to delete all owned address components address_id=%v: %w", insertedAddress.ID, err)
				}

				// Insert new components
				for _, component := range *address.AddressComponents {
					_, err = qtx.CreateAddressComponent(ctx, db.CreateAddressComponentParams{
						Address: insertedAddress.ID,
						Seq:     component.Seq,
						Type:    string(component.Type),
						Value:   component.Value,
					})
					if err != nil {
						return nil, fmt.Errorf("failed to create address component type=%v seq=%v: %w", component.Type, component.Seq, err)
					}
				}
			}
		}
	} else if request.Body.Addresses.IsNull() {
		err = qtx.DeleteAllOwnedAddresses(ctx, orig.ID)
		if err != nil {
			return nil, fmt.Errorf("failed to delete all owned addresses entry_id=%v: %w", orig.ID, err)
		}
	}

	if illConfig, ok := patchValue(request.Body.IllConfig); ok {
		lenders, _ := patchValue(illConfig.LendersOfLastResort)
		validationMessage, validationErr := validateLendersOfLastResort(ctx, qtx, lenders)
		if validationErr != nil {
			return nil, fmt.Errorf("failed to validate lenders of last resort: %w", validationErr)
		}
		if validationMessage != "" {
			return UpdateEntry400JSONResponse{badRequest(validationMessage)}, nil
		}
	}
	configs := maps.Clone(orig.Configs)
	changed, message, err := writeContainers(configs, supplied, false)
	if err != nil {
		return nil, fmt.Errorf("unable to merge configuration: %w", err)
	}
	if message != "" {
		return UpdateEntry400JSONResponse{badRequest(message)}, nil
	}
	if changed {
		if err := qtx.UpdateEntryConfigs(ctx, db.UpdateEntryConfigsParams{ID: orig.ID, Configs: configs}); err != nil {
			return nil, fmt.Errorf("unexpected database error during configuration update: %w", err)
		}
	}

	err = tx.Commit(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	return UpdateEntry204Response{}, nil
}

func (a ApiImpl) DeleteEntry(ctx context.Context, request DeleteEntryRequestObject) (DeleteEntryResponseObject, error) {
	authData := auth.GetAuthData(ctx)
	validRoles := []auth.DirectoryRole{auth.ConsortialAdminRole}
	if !authData.HasRoleFromList(validRoles) {
		slog.DebugContext(ctx, "permission denied")
		return DeleteEntry403JSONResponse{forbidden("Access denied")}, nil
	}

	var err error
	switch request.Key {
	case DeleteEntryParamsKeyById:
		parsedId, perr := uuid.Parse(request.Value)
		if perr != nil {
			return DeleteEntry400JSONResponse{badRequest(invalidIDMessage)}, nil
		}
		err = a.queries.DeleteEntryById(ctx, parsedId)
	case DeleteEntryParamsKeyBySymbol:
		authority, symbol, perr := resolveCombinedSymbol(request.Value)
		if perr != nil {
			return DeleteEntry400JSONResponse{badRequest(invalidSymbolMessage)}, nil
		}
		err = a.queries.DeleteEntryBySymbol(ctx, db.DeleteEntryBySymbolParams{Authority: authority, Symbol: symbol})
	}
	if err != nil {
		return nil, fmt.Errorf("failed to delete entry: %w", err)
	}
	return DeleteEntry204Response{}, nil
}
