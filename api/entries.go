package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/google/uuid"
	"github.com/indexdata/cql-go/cql"
	"github.com/indexdata/cql-go/pgcql"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"indexdata/directory/auth"
	"indexdata/directory/db"
)

const defaultSymbolAuthority string = "TEST"
const tenantSymbolAuthorityEnv string = "TENANT_SYMBOL_AUTHORITY"

func getSymbolAuthority() string {
	symbolAuthority := os.Getenv(tenantSymbolAuthorityEnv)
	if symbolAuthority == "" {
		return defaultSymbolAuthority
	}
	return symbolAuthority
}

func scanEntryRow(rows pgx.Rows) (Entry, int, error) {
	var (
		id              uuid.UUID
		name            string
		description     *string
		contactName     *string
		organizationId  *string
		email           *string
		phoneNumber     *string
		lmsLocationCode *string
		lmsConfigJSON   []byte
		hrid            *string
		timeZone        *string
		entryType       *string
		parent          *uuid.UUID
		symbolsJSON     [][]byte
		endpointsJSON   [][]byte
		addressesJSON   [][]byte
		tiersJSON       [][]byte
		networksJSON    [][]byte
		closuresJSON    [][]byte
		totalCount      int
	)

	if err := rows.Scan(&id, &name, &description, &organizationId, &contactName, &email, &phoneNumber,
		&lmsLocationCode, &lmsConfigJSON, &hrid, &timeZone, &entryType, &parent, &symbolsJSON, &endpointsJSON,
		&addressesJSON, &tiersJSON, &networksJSON, &closuresJSON, &totalCount); err != nil {
		return Entry{}, 0, err
	}

	symbols, err := unmarshalJSONArray[Symbol](symbolsJSON)
	if err != nil {
		return Entry{}, 0, fmt.Errorf("unmarshalling symbols: %w", err)
	}

	endpoints, err := unmarshalJSONArray[ServiceEndpoint](endpointsJSON)
	if err != nil {
		return Entry{}, 0, fmt.Errorf("unmarshalling endpoints: %w", err)
	}

	addresses, err := unmarshalJSONArray[Address](addressesJSON)
	if err != nil {
		return Entry{}, 0, fmt.Errorf("unmarshalling addresses: %w", err)
	}

	closures, err := unmarshalJSONArray[Closure](closuresJSON)
	if err != nil {
		return Entry{}, 0, fmt.Errorf("unmarshalling closures: %w", err)
	}

	lmsConfig, err := unmarshalJSONObject[LmsConfig](lmsConfigJSON)
	if err != nil {
		return Entry{}, 0, fmt.Errorf("unmarshalling lms config: %w", err)
	}

	tiers, err := unmarshalJSONArray[Tier](tiersJSON)
	if err != nil {
		return Entry{}, 0, fmt.Errorf("unmarshalling tiers: %w", err)
	}

	networks, err := unmarshalJSONArray[Network](networksJSON)
	if err != nil {
		return Entry{}, 0, fmt.Errorf("unmarshalling networks config: %w", err)
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

	var networksPtr *[]Network
	if len(networks) > 0 {
		networksPtr = &networks
	}

	lmsConfigPtr := lmsConfig

	typeValue := EntryType(*entryType)

	return Entry{
		Id:              &id,
		Name:            name,
		Type:            &typeValue,
		OrganizationId:  organizationId,
		Description:     description,
		ContactName:     contactName,
		Email:           email,
		Hrid:            hrid,
		LmsLocationCode: lmsLocationCode,
		PhoneNumber:     phoneNumber,
		Parent:          parent,
		Symbols:         symbolsPtr,
		Endpoints:       endpointsPtr,
		Addresses:       addressesPtr,
		Closures:        closuresPtr,
		LmsConfig:       lmsConfigPtr,
		Tiers:           tiersPtr,
		Networks:        networksPtr,
		TimeZone:        timeZone,
	}, totalCount, nil
}

const defaultEntryOrder = "ORDER BY e.name, e.id"
const defaultEntryLimit = 10

// handleEntryCQL converts a CQL query string to a PostgreSQL WHERE clause
func handleEntryCQL(cqlString string, noBaseArgs int) (pgcql.Query, error) {
	def := pgcql.NewPgDefinition()

	f := pgcql.NewFieldString().WithLikeOps()
	f.SetColumn("e.name")
	def.AddField("name", f)

	f = pgcql.NewFieldString().WithLikeOps()
	f.SetColumn("e.description")
	def.AddField("description", f)

	var parser cql.Parser
	query, err := parser.Parse(cqlString)
	if err != nil {
		return nil, err
	}
	return def.Parse(query, noBaseArgs+1)
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
			e.phone_number,
			e.lms_location_code,
			(SELECT 
				json_build_object(
					'acceptItemEnabled', l.accept_item_enabled,
					'address',l.address, 
					'checkInItemEnabled', l.checkin_item_enabled,
					'checkOutItemEnabled', l.checkout_item_enabled,
					'fromAgency',l.from_agency,
					'fromAgencyAuthentication', l.from_agency_authentication,
					'itemLocation', l.item_location,
					'lookupUserEnabled', l.lookup_user_enabled,
					'requestItemBibIdCode', l.request_item_bib_code,
					'requestItemPickupLocationEnabled', l.request_item_pickup_location_enabled,
					'requestItemRequestScopeType', l.request_item_scope_type,
					'requestItemRequestType', l.request_item_request_type,
					'requesterPatronPattern', l.requester_patron_pattern,
					'requesterPickupLocation', l.requester_pickup_location,
					'supplierPickupLocation', l.supplier_pickup_location,
					'toAgency', l.to_agency
				) from lms_configs l WHERE l.entry = e.id) as lms_config,
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
			ARRAY(SELECT row_to_json(t) FROM memberships m
				JOIN membership_tiers mt on mt.membership = m.id
				JOIN tiers t on t.id = mt.tier
				WHERE m.institution = e.id) as tiers,
            ARRAY(SELECT row_to_json(n) FROM memberships m
				JOIN membership_networks mn on mn.membership = m.id
				JOIN networks n on n.id = mn.network
				WHERE m.institution = e.id) as networks,
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
				ORDER BY c.id
			) as closures,
			COUNT(*) OVER() as total_count
		FROM entries e
	`
	if whereClause != "" {
		return baseQuery + "\n" + whereClause
	}
	return baseQuery
}

func (a ApiImpl) GetEntries(ctx context.Context, request GetEntriesRequestObject) (GetEntriesResponseObject, error) {
	var query string
	var args []interface{}

	authData := auth.GetAuthData(ctx)
	validRoles := []auth.DirectoryRole{auth.ConsortialAdminRole, auth.InstitutionalAdminRole, auth.PublicUserRole}
	seeSensitiveRoles := []auth.DirectoryRole{auth.ConsortialAdminRole, auth.SystemUserRole}
	if !authData.HasRoleFromList(validRoles) {
		slog.ErrorContext(ctx, "permission denied")
		return GetEntries401TextResponse("Access denied"), nil
	}

	ourEntry, _ := a.queries.EntryBySymbol(ctx,
		db.EntryBySymbolParams{Authority: getSymbolAuthority(), Symbol: authData.GetInstitution()})

	if request.Params.Q != nil && *request.Params.Q != "" {
		// Use CQL query
		noBaseArgs := 0
		res, err := handleEntryCQL(*request.Params.Q, noBaseArgs)
		if err != nil {
			return GetEntries400TextResponse(fmt.Sprintf("CQL parse error: %v", err)), nil
		}

		whereClause := ""
		if res.GetWhereClause() != "" {
			whereClause = "WHERE " + res.GetWhereClause()
		}

		query = buildEntrySQL(whereClause + "\n" + defaultEntryOrder)
		args = res.GetQueryArguments()
	} else {
		query = buildEntrySQL(defaultEntryOrder)
		args = []interface{}{}
	}

	// Add LIMIT clause
	limit := defaultEntryLimit
	if request.Params.Limit != nil {
		limit = int(*request.Params.Limit)
	}
	args = append(args, limit)
	query += fmt.Sprintf("\nLIMIT $%d", len(args))

	// Add OFFSET clause if provided
	if request.Params.Offset != nil {
		args = append(args, *request.Params.Offset)
		query += fmt.Sprintf("\nOFFSET $%d", len(args))
	}

	rows, err := a.pool.Query(ctx, query, args...)
	if err != nil {
		slog.ErrorContext(ctx, "failed to query entries", "error", err)
		return GetEntries500TextResponse("Internal server error"), nil
	}
	defer rows.Close()

	// Need to initialise items as explicitly zero length because a simple
	// var items []Entry will be JSON-encoded as null rather than [].
	// See https://github.com/golang/go/issues/27589
	items := make([]Entry, 0)
	var totalCount int

	for rows.Next() {

		entry, count, err := scanEntryRow(rows)
		seeSensitive := (ourEntry.ID.String() == entry.Id.String()) || authData.HasRoleFromList(seeSensitiveRoles)

		if err != nil {
			slog.ErrorContext(ctx, "failed to scan entry row", "error", err)
			return GetEntries500TextResponse("Internal server error"), nil
		}
		if !seeSensitive {
			err = sanitizeEntry(&entry)
			if err != nil {
				slog.ErrorContext(ctx, "error sanitizing entry", "error", err)
				return GetEntries500TextResponse("Internal server error"), nil
			}
		}

		items = append(items, entry)
		totalCount = count
	}

	if err := rows.Err(); err != nil {
		slog.ErrorContext(ctx, "error iterating entry rows", "error", err)
		return GetEntries500TextResponse("Internal server error"), nil
	}

	// Build response with pagination info
	response := EntriesResponse{
		Items: items,
		About: About{
			Count: int64(totalCount),
		},
	}

	return GetEntries200JSONResponse(response), nil
}

func (a ApiImpl) GetEntry(ctx context.Context, request GetEntryRequestObject) (GetEntryResponseObject, error) {
	var query string
	var args []interface{}
	var seeSensitive bool

	authData := auth.GetAuthData(ctx)

	validRoles := []auth.DirectoryRole{auth.ConsortialAdminRole, auth.InstitutionalAdminRole, auth.SystemUserRole, auth.PublicUserRole}
	seeSensitiveRoles := []auth.DirectoryRole{auth.ConsortialAdminRole, auth.SystemUserRole}
	if !authData.HasRoleFromList(validRoles) {
		slog.ErrorContext(ctx, "permission denied")
		return GetEntry401TextResponse("Access denied"), nil
	}

	ownSymbolInstitution := authData.GetInstitution()
	ownSymbolAuthority := getSymbolAuthority()

	ownedEntry, err := a.queries.EntryBySymbol(ctx,
		db.EntryBySymbolParams{Authority: ownSymbolAuthority, Symbol: ownSymbolInstitution})

	if err != nil {
		slog.ErrorContext(ctx, "Unable to get entry by symbol", "authority", ownSymbolAuthority, "institution", ownSymbolInstitution, "error", err)
	}

	if request.Key == GetEntryParamsKeyById {
		parsedId, perr := uuid.Parse(request.Value)
		if perr != nil {
			return GetEntry400TextResponse("Error parsing id"), nil
		}
		query = buildEntrySQL("WHERE e.id = $1")
		args = []interface{}{parsedId}
	} else if request.Key == GetEntryParamsKeyBySymbol {
		authority, symbol, perr := resolveCombinedSymbol(request.Value)
		if perr != nil {
			return GetEntry400TextResponse("No delimiter found or other issue parsing symbol"), nil
		}
		query = buildEntrySQL(`
			WHERE e.id = (
				SELECT owner FROM symbols WHERE authority = $1 AND symbol = $2
			)
		`)
		args = []interface{}{authority, symbol}
	}

	rows, err := a.pool.Query(ctx, query, args...)
	if err != nil {
		slog.ErrorContext(ctx, "failed to query entry", "error", err)
		return GetEntry500TextResponse("Internal server error"), nil
	}
	defer rows.Close()

	if !rows.Next() {
		return GetEntry404TextResponse("Entry not found"), nil
	}

	entry, _, err := scanEntryRow(rows)
	if err != nil {
		slog.ErrorContext(ctx, "failed to scan entry row", "error", err)
		return GetEntry500TextResponse("Internal server error"), nil
	}

	if ownedEntry.ID.String() == entry.Id.String() || authData.HasRoleFromList(seeSensitiveRoles) {
		seeSensitive = true
	}

	if !seeSensitive {
		err := sanitizeEntry(&entry)
		if err != nil {
			slog.ErrorContext(ctx, "error sanitizing protected fields", "error", err)
			return GetEntry500TextResponse("Internal server error"), nil
		}
	}

	return GetEntry200JSONResponse(entry), nil
}

func sanitizeEntry(entry *Entry) error {
	return Sanitize(entry)

}

func (a ApiImpl) AddEntry(ctx context.Context, request AddEntryRequestObject) (AddEntryResponseObject, error) {

	authData := auth.GetAuthData(ctx)
	validRoles := []auth.DirectoryRole{auth.ConsortialAdminRole}
	if !authData.HasRoleFromList(validRoles) {
		slog.ErrorContext(ctx, "permission denied")
		return AddEntry401TextResponse("Access denied"), nil
	}

	tx, err := a.pool.Begin(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "failed to begin transaction", "error", err)
		return AddEntry500TextResponse("Internal server error"), nil
	}
	defer func() { _ = tx.Rollback(ctx) }()
	qtx := a.queries.WithTx(tx)

	toInsert := db.CreateEntryParams{
		Name:            request.Body.Name,
		ContactName:     request.Body.ContactName,
		Email:           request.Body.Email,
		PhoneNumber:     request.Body.PhoneNumber,
		TimeZone:        request.Body.TimeZone,
		OrganizationID:  request.Body.OrganizationId,
		Type:            string(derefOrDefault(request.Body.Type, "Institution")),
		Parent:          request.Body.Parent,
		LmsLocationCode: request.Body.LmsLocationCode,
	}
	insertedEntry, err := qtx.CreateEntry(ctx, toInsert)
	if err != nil {
		slog.ErrorContext(ctx, "failed to create entry", "error", err, "name", request.Body.Name)
		return AddEntry500TextResponse("Internal server error"), nil
	}

	if request.Body.Symbols != nil {
		for _, symbol := range *request.Body.Symbols {
			_, err = qtx.UpsertSymbol(ctx, db.UpsertSymbolParams{
				Owner:     insertedEntry.ID,
				Symbol:    strings.ToUpper(symbol.Symbol),
				Authority: strings.ToUpper(symbol.Authority),
			})
			if err != nil {
				var pge *pgconn.PgError
				if errors.As(err, &pge) {
					if pge.SQLState() == "23505" { //unique_violation
						slog.InfoContext(ctx, "duplicate symbol rejected", "symbol", symbol.Symbol, "authority", symbol.Authority)
						return AddEntry400TextResponse("Duplicate symbol"), nil
					}
				}
				slog.ErrorContext(ctx, "failed to create symbol", "error", err, "symbol", symbol.Symbol, "authority", symbol.Authority)
				return AddEntry500TextResponse("Internal server error"), nil
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
				slog.ErrorContext(ctx, "failed to create service endpoint", "error", err, "name", endpoint.Name, "type", endpoint.Type)
				return AddEntry500TextResponse("Internal server error"), nil
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
				slog.ErrorContext(ctx, "failed to upsert address", "error", err, "type", address.Type)
				return AddEntry500TextResponse("Internal server error"), nil
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
						slog.ErrorContext(ctx, "failed to create address component", "error", err, "type", component.Type, "seq", component.Seq)
						return AddEntry500TextResponse("Internal server error"), nil
					}
				}
			}
		}
	}

	if request.Body.LmsConfig != nil {
		lmsConfig := request.Body.LmsConfig
		_, err := qtx.UpsertLMSConfig(ctx, db.UpsertLMSConfigParams{
			Entry:                            &insertedEntry.ID,
			Address:                          lmsConfig.Address,
			FromAgency:                       lmsConfig.FromAgency,
			FromAgencyAuthentication:         lmsConfig.FromAgencyAuthentication,
			ToAgency:                         lmsConfig.ToAgency,
			LookupUserEnabled:                lmsConfig.LookupUserEnabled,
			AcceptItemEnabled:                lmsConfig.AcceptItemEnabled,
			CheckinItemEnabled:               lmsConfig.CheckInItemEnabled,
			CheckoutItemEnabled:              lmsConfig.CheckOutItemEnabled,
			ItemLocation:                     lmsConfig.ItemLocation,
			RequestItemRequestType:           lmsConfig.RequestItemRequestType,
			RequestItemScopeType:             lmsConfig.RequestItemRequestScopeType,
			RequestItemBibCode:               lmsConfig.RequestItemBibIdCode,
			RequestItemPickupLocationEnabled: lmsConfig.RequestItemPickupLocationEnabled,
			RequesterPickupLocation:          lmsConfig.RequesterPickupLocation,
			RequesterPatronPattern:           lmsConfig.RequesterPatronPattern,
			SupplierPickupLocation:           lmsConfig.SupplierPickupLocation,
		})
		if err != nil {
			slog.ErrorContext(ctx, "failed to create lmsConfig component", "error", err, "to_agency", lmsConfig.ToAgency)
			return AddEntry500TextResponse("Internal server error"), nil
		}
	}

	var resp Id
	resp.Id = insertedEntry.ID

	err = tx.Commit(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "failed to commit transaction", "error", err)
		return AddEntry500TextResponse("Internal server error"), nil
	}

	return AddEntry201JSONResponse(resp), nil
}

func (a ApiImpl) UpdateEntry(ctx context.Context, request UpdateEntryRequestObject) (UpdateEntryResponseObject, error) {

	authData := auth.GetAuthData(ctx)
	validRoles := []auth.DirectoryRole{auth.ConsortialAdminRole, auth.InstitutionalAdminRole}
	writeRoles := []auth.DirectoryRole{auth.ConsortialAdminRole}

	if !authData.HasRoleFromList(validRoles) {
		slog.ErrorContext(ctx, "permission denied")
		return UpdateEntry401TextResponse("Access denied"), nil
	}

	ownedEntry, _ := a.queries.EntryBySymbol(ctx,
		db.EntryBySymbolParams{Authority: getSymbolAuthority(), Symbol: authData.GetInstitution()})

	tx, err := a.pool.Begin(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "failed to begin transaction", "error", err)
		return UpdateEntry500TextResponse("Internal server error"), nil
	}
	defer func() { _ = tx.Rollback(ctx) }()
	qtx := a.queries.WithTx(tx)

	var orig db.Entry
	if request.Key == UpdateEntryParamsKeyById {
		parsedId, perr := uuid.Parse(request.Value)
		if perr != nil {
			return UpdateEntry400TextResponse("Error parsing id"), nil
		}
		orig, err = qtx.EntryByIdForUpdate(ctx, parsedId)
	} else if request.Key == UpdateEntryParamsKeyBySymbol {
		authority, symbol, perr := resolveCombinedSymbol(request.Value)
		if perr != nil {
			return UpdateEntry400TextResponse("No delimiter found or other issue parsing symbol"), nil
		}
		orig, err = qtx.EntryBySymbolForUpdate(ctx, db.EntryBySymbolForUpdateParams{Authority: authority, Symbol: symbol})
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return UpdateEntry404TextResponse("Entry not found"), nil
	} else if err != nil {
		slog.ErrorContext(ctx, "failed to fetch entry for update", "error", err)
		return UpdateEntry500TextResponse("Internal server error"), nil
	}

	if !authData.HasRoleFromList(writeRoles) && ownedEntry.ID.String() != orig.ID.String() {
		slog.ErrorContext(ctx, "permission denied")
		return UpdateEntry401TextResponse("Access denied"), nil
	}

	if request.Body.Type.IsSpecified() && request.Body.Type.IsNull() {
		slog.ErrorContext(ctx, "type cannot be null")
		return UpdateEntry400TextResponse("'type' cannot be set to null"), nil
	}

	origTypeEntryPatch := EntryPatchType(orig.Type)

	err = qtx.UpdateEntry(ctx, db.UpdateEntryParams{
		Name:            derefOrDefault(request.Body.Name, orig.Name),
		Description:     maybeUpdateCol(orig.Description, request.Body.Description),
		ContactName:     maybeUpdateCol(orig.ContactName, request.Body.ContactName),
		Email:           maybeUpdateCol(orig.Email, request.Body.Email),
		PhoneNumber:     maybeUpdateCol(orig.PhoneNumber, request.Body.PhoneNumber),
		Parent:          maybeUpdateCol(orig.Parent, request.Body.Parent),
		LmsLocationCode: maybeUpdateCol(orig.LmsLocationCode, request.Body.LmsLocationCode),
		Hrid:            maybeUpdateCol(orig.Hrid, request.Body.Hrid),
		Type:            string(*maybeUpdateCol(&origTypeEntryPatch, request.Body.Type)),
		TimeZone:        maybeUpdateCol(orig.TimeZone, request.Body.TimeZone),
		OrganizationID:  maybeUpdateCol(orig.OrganizationID, request.Body.OrganizationId),
		ID:              orig.ID,
	})
	if err != nil {
		slog.ErrorContext(ctx, "failed to update entry", "error", err, "id", orig.ID)
		return UpdateEntry500TextResponse("Internal server error"), nil
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
				slog.ErrorContext(ctx, "failed to delete other owned symbols", "error", err, "entry_id", orig.ID)
				return UpdateEntry500TextResponse("Internal server error"), nil
			}
		} else {
			err = qtx.DeleteAllOwnedSymbols(ctx, orig.ID)
			if err != nil {
				slog.ErrorContext(ctx, "failed to delete all owned symbols", "error", err, "entry_id", orig.ID)
				return UpdateEntry500TextResponse("Internal server error"), nil
			}
		}

		// Update/create symbols
		for _, symbol := range reqsyms {
			_, err = qtx.UpsertSymbol(ctx, db.UpsertSymbolParams{
				ID:        symbol.Id,
				Owner:     orig.ID,
				Symbol:    strings.ToUpper(symbol.Symbol),
				Authority: strings.ToUpper(symbol.Authority),
			})
			if err != nil {
				var pge *pgconn.PgError
				if errors.As(err, &pge) {
					if pge.SQLState() == "23505" { //unique_violation
						slog.InfoContext(ctx, "duplicate symbol rejected", "symbol", symbol.Symbol, "authority", symbol.Authority)
						return UpdateEntry400TextResponse("Duplicate symbol"), nil
					}
				}
				slog.ErrorContext(ctx, "unexpected database error during symbol upsert", "error", err, "symbol", symbol.Symbol, "authority", symbol.Authority)
				return UpdateEntry500TextResponse("Internal server error"), nil
			}
		}
	} else if request.Body.Symbols.IsNull() {
		err = qtx.DeleteAllOwnedSymbols(ctx, orig.ID)
		if err != nil {
			slog.ErrorContext(ctx, "failed to delete all owned symbols", "error", err, "entry_id", orig.ID)
			return UpdateEntry500TextResponse("Internal server error"), nil
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
				slog.ErrorContext(ctx, "failed to delete other owned service endpoints", "error", err, "entry_id", orig.ID)
				return UpdateEntry500TextResponse("Internal server error"), nil
			}
		} else {
			err = qtx.DeleteAllOwnedServiceEndpoints(ctx, orig.ID)
			if err != nil {
				slog.ErrorContext(ctx, "failed to delete all owned service endpoints", "error", err, "entry_id", orig.ID)
				return UpdateEntry500TextResponse("Internal server error"), nil
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
				slog.ErrorContext(ctx, "failed to upsert service endpoint", "error", err, "name", endpoint.Name, "type", endpoint.Type)
				return UpdateEntry500TextResponse("Internal server error"), nil
			}
		}
	} else if request.Body.Endpoints.IsNull() {
		err = qtx.DeleteAllOwnedServiceEndpoints(ctx, orig.ID)
		if err != nil {
			slog.ErrorContext(ctx, "failed to delete all owned service endpoints", "error", err, "entry_id", orig.ID)
			return UpdateEntry500TextResponse("Internal server error"), nil
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
				slog.ErrorContext(ctx, "failed to delete other owned addresses", "error", err, "entry_id", orig.ID)
				return UpdateEntry500TextResponse("Internal server error"), nil
			}
		} else {
			err = qtx.DeleteAllOwnedAddresses(ctx, orig.ID)
			if err != nil {
				slog.ErrorContext(ctx, "failed to delete all owned addresses", "error", err, "entry_id", orig.ID)
				return UpdateEntry500TextResponse("Internal server error"), nil
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
				slog.ErrorContext(ctx, "failed to upsert address", "error", err, "type", address.Type)
				return UpdateEntry500TextResponse("Internal server error"), nil
			}

			// Handle address components
			if address.AddressComponents != nil {
				// Delete all existing components and insert new ones
				err = qtx.DeleteAllOwnedAddressComponents(ctx, insertedAddress.ID)
				if err != nil {
					slog.ErrorContext(ctx, "failed to delete all owned address components", "error", err, "address_id", insertedAddress.ID)
					return UpdateEntry500TextResponse("Internal server error"), nil
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
						slog.ErrorContext(ctx, "failed to create address component", "error", err, "type", component.Type, "seq", component.Seq)
						return UpdateEntry500TextResponse("Internal server error"), nil
					}
				}
			}
		}
	} else if request.Body.Addresses.IsNull() {
		err = qtx.DeleteAllOwnedAddresses(ctx, orig.ID)
		if err != nil {
			slog.ErrorContext(ctx, "failed to delete all owned addresses", "error", err, "entry_id", orig.ID)
			return UpdateEntry500TextResponse("Internal server error"), nil
		}
	}

	if request.Body.LmsConfig.IsSpecified() && !request.Body.LmsConfig.IsNull() {
		lmsConfig := request.Body.LmsConfig.MustGet()

		if err != nil {
			slog.ErrorContext(ctx, "unable to query original LMS Config", "error", err)
			return UpdateEntry500TextResponse("Internal server error"), nil
		}

		originalLMSConfig, _ := qtx.GetLMSConfigByEntry(ctx, orig.ID)

		_, err = qtx.UpsertLMSConfig(ctx, db.UpsertLMSConfigParams{
			Entry:                            &orig.ID,
			Address:                          derefOrDefault(lmsConfig.Address, originalLMSConfig.Address),
			FromAgency:                       derefOrDefault(lmsConfig.FromAgency, originalLMSConfig.FromAgency),
			FromAgencyAuthentication:         derefOrDefaultPtr(lmsConfig.FromAgencyAuthentication, originalLMSConfig.FromAgencyAuthentication),
			ToAgency:                         derefOrDefaultPtr(lmsConfig.ToAgency, originalLMSConfig.ToAgency),
			LookupUserEnabled:                derefOrDefaultPtr(lmsConfig.LookupUserEnabled, originalLMSConfig.LookupUserEnabled),
			AcceptItemEnabled:                derefOrDefaultPtr(lmsConfig.AcceptItemEnabled, originalLMSConfig.AcceptItemEnabled),
			CheckinItemEnabled:               derefOrDefaultPtr(lmsConfig.CheckInItemEnabled, originalLMSConfig.CheckinItemEnabled),
			CheckoutItemEnabled:              derefOrDefaultPtr(lmsConfig.CheckOutItemEnabled, originalLMSConfig.CheckoutItemEnabled),
			ItemLocation:                     derefOrDefaultPtr(lmsConfig.ItemLocation, originalLMSConfig.ItemLocation),
			RequestItemRequestType:           derefOrDefaultPtr(lmsConfig.RequestItemRequestType, originalLMSConfig.RequestItemRequestType),
			RequestItemScopeType:             derefOrDefaultPtr(lmsConfig.RequestItemRequestScopeType, originalLMSConfig.RequestItemScopeType),
			RequestItemBibCode:               derefOrDefaultPtr(lmsConfig.RequestItemBibIdCode, originalLMSConfig.RequestItemBibCode),
			RequestItemPickupLocationEnabled: derefOrDefaultPtr(lmsConfig.RequestItemPickupLocationEnabled, originalLMSConfig.RequestItemPickupLocationEnabled),
			RequesterPickupLocation:          derefOrDefaultPtr(lmsConfig.RequesterPickupLocation, originalLMSConfig.RequesterPickupLocation),
			SupplierPickupLocation:           derefOrDefaultPtr(lmsConfig.SupplierPickupLocation, originalLMSConfig.SupplierPickupLocation),
			RequesterPatronPattern:           derefOrDefaultPtr(lmsConfig.RequesterPatronPattern, originalLMSConfig.RequesterPatronPattern),
		})
		if err != nil {
			slog.ErrorContext(ctx, "unexpected database error during lmsConfig upsert", "error", err)
			return UpdateEntry500TextResponse(err.Error()), nil
		}
	}

	err = tx.Commit(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "failed to commit transaction", "error", err)
		return UpdateEntry500TextResponse("Internal server error"), nil
	}

	return UpdateEntry204Response{}, nil
}

func (a ApiImpl) DeleteEntry(ctx context.Context, request DeleteEntryRequestObject) (DeleteEntryResponseObject, error) {
	authData := auth.GetAuthData(ctx)
	validRoles := []auth.DirectoryRole{auth.ConsortialAdminRole}
	if !authData.HasRoleFromList(validRoles) {
		slog.ErrorContext(ctx, "permission denied")
		return DeleteEntry401TextResponse("Access denied"), nil
	}

	var err error
	if request.Key == DeleteEntryParamsKeyById {
		parsedId, perr := uuid.Parse(request.Value)
		if perr != nil {
			return DeleteEntry400TextResponse("Error parsing id"), nil
		}
		err = a.queries.DeleteEntryById(ctx, parsedId)
	} else if request.Key == DeleteEntryParamsKeyBySymbol {
		authority, symbol, perr := resolveCombinedSymbol(request.Value)
		if perr != nil {
			return DeleteEntry400TextResponse("No delimiter found or other issue parsing symbol"), nil
		}
		err = a.queries.DeleteEntryBySymbol(ctx, db.DeleteEntryBySymbolParams{Authority: authority, Symbol: symbol})
	}
	if err != nil {
		slog.ErrorContext(ctx, "failed to delete entry", "error", err)
		return DeleteEntry500TextResponse("Internal server error"), nil
	}
	return DeleteEntry204Response{}, nil
}
