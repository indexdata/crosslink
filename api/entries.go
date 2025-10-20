package api

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"

	"github.com/google/uuid"
	"github.com/indexdata/cql-go/cql"
	"github.com/indexdata/cql-go/pgcql"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"indexdata/directoryish/db"
)

func scanEntryRow(rows pgx.Rows) (Entry, error) {
	var (
		id            uuid.UUID
		name          string
		description   *string
		contactName   *string
		email         *string
		symbolsJSON   [][]byte
		endpointsJSON [][]byte
		addressesJSON [][]byte
	)

	if err := rows.Scan(&id, &name, &description, &contactName, &email, &symbolsJSON, &endpointsJSON, &addressesJSON); err != nil {
		return Entry{}, err
	}

	symbols, err := unmarshalJSONArray[Symbol](symbolsJSON)
	if err != nil {
		return Entry{}, fmt.Errorf("unmarshalling symbols: %w", err)
	}

	endpoints, err := unmarshalJSONArray[ServiceEndpoint](endpointsJSON)
	if err != nil {
		return Entry{}, fmt.Errorf("unmarshalling endpoints: %w", err)
	}

	addresses, err := unmarshalJSONArray[Address](addressesJSON)
	if err != nil {
		return Entry{}, fmt.Errorf("unmarshalling addresses: %w", err)
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

	return Entry{
		Id:          &id,
		Name:        name,
		Description: description,
		ContactName: contactName,
		Email:       email,
		Symbols:     symbolsPtr,
		Endpoints:   endpointsPtr,
		Addresses:   addressesPtr,
	}, nil
}

const defaultEntryOrder = "ORDER BY e.name, e.id"

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
			e.contact_name,
			e.email,
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
			) as addresses
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

	rows, err := a.pool.Query(ctx, query, args...)
	if err != nil {
		log.Fatal(err)
	}
	defer rows.Close()

	// Need to initialise resp as explicitly zero length because a simple
	// var resp []Entry will be JSON-encoded as null rather than [].
	// See https://github.com/golang/go/issues/27589
	resp := make([]Entry, 0)

	for rows.Next() {
		entry, err := scanEntryRow(rows)
		if err != nil {
			log.Fatal(err)
		}
		resp = append(resp, entry)
	}

	if err := rows.Err(); err != nil {
		log.Fatal(err)
	}

	return GetEntries200JSONResponse(resp), nil
}

func (a ApiImpl) GetEntry(ctx context.Context, request GetEntryRequestObject) (GetEntryResponseObject, error) {
	var query string
	var args []interface{}

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
		log.Fatal(err)
	}
	defer rows.Close()

	if !rows.Next() {
		return GetEntry404TextResponse("Entry not found"), nil
	}

	entry, err := scanEntryRow(rows)
	if err != nil {
		log.Fatal(err)
	}

	return GetEntry200JSONResponse(entry), nil
}

func (a ApiImpl) AddEntry(ctx context.Context, request AddEntryRequestObject) (AddEntryResponseObject, error) {
	tx, err := a.pool.Begin(ctx)
	if err != nil {
		log.Fatal(err)
	}
	defer tx.Rollback(ctx)
	qtx := a.queries.WithTx(tx)

	toInsert := db.CreateEntryParams{
		Name:        request.Body.Name,
		ContactName: request.Body.ContactName,
		Email:       request.Body.Email,
	}
	insertedEntry, err := qtx.CreateEntry(ctx, toInsert)
	if err != nil {
		log.Fatal(err)
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
						return AddEntry400TextResponse("Duplicate symbol"), nil
					}
				}
				log.Fatal(err)
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
				log.Fatal(err)
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
				log.Fatal(err)
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
						log.Fatal(err)
					}
				}
			}
		}
	}

	var resp Id
	resp.Id = insertedEntry.ID

	err = tx.Commit(ctx)
	if err != nil {
		log.Fatal(err)
	}

	return AddEntry201JSONResponse(resp), nil
}

func (a ApiImpl) UpdateEntry(ctx context.Context, request UpdateEntryRequestObject) (UpdateEntryResponseObject, error) {
	var orig db.Entry
	var err error
	if request.Key == UpdateEntryParamsKeyById {
		parsedId, perr := uuid.Parse(request.Value)
		if perr != nil {
			return UpdateEntry400TextResponse("Error parsing id"), nil
		}
		orig, err = a.queries.EntryById(ctx, parsedId)
		if err != nil {
			print(err.Error())
		}
	} else if request.Key == UpdateEntryParamsKeyBySymbol {
		authority, symbol, perr := resolveCombinedSymbol(request.Value)
		if perr != nil {
			return UpdateEntry400TextResponse("No delimiter found or other issue parsing symbol"), nil
		}
		orig, err = a.queries.EntryBySymbol(ctx, db.EntryBySymbolParams{Authority: authority, Symbol: symbol})
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return UpdateEntry404TextResponse("Entry not found"), nil
	} else if err != nil {
		log.Fatal(err)
	}

	tx, err := a.pool.Begin(ctx)
	if err != nil {
		log.Fatal(err)
	}
	defer tx.Rollback(ctx)
	qtx := a.queries.WithTx(tx)

	err = qtx.UpdateEntry(ctx, db.UpdateEntryParams{
		Name:        derefOrDefault(request.Body.Name, orig.Name),
		ContactName: maybeUpdateCol(orig.ContactName, request.Body.ContactName),
		Email:       maybeUpdateCol(orig.Email, request.Body.Email),
		ID:          orig.ID,
	})
	if err != nil {
		log.Fatal(err)
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
			qtx.DeleteOtherOwnedSymbols(ctx, db.DeleteOtherOwnedSymbolsParams{Owner: orig.ID, Ids: patchedSymbols})
		} else {
			qtx.DeleteAllOwnedSymbols(ctx, orig.ID)
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
				log.Println(err)
				var pge *pgconn.PgError
				if errors.As(err, &pge) {
					if pge.SQLState() == "23505" { //unique_violation
						return UpdateEntry400TextResponse("Duplicate symbol"), nil
					}
				}
				log.Fatal(err)
			}
		}
	} else if request.Body.Symbols.IsNull() {
		qtx.DeleteAllOwnedSymbols(ctx, orig.ID)
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
			qtx.DeleteOtherOwnedServiceEndpoints(ctx, db.DeleteOtherOwnedServiceEndpointsParams{Entry: orig.ID, Ids: patchedEndpoints})
		} else {
			qtx.DeleteAllOwnedServiceEndpoints(ctx, orig.ID)
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
				log.Fatal(err)
			}
		}
	} else if request.Body.Endpoints.IsNull() {
		qtx.DeleteAllOwnedServiceEndpoints(ctx, orig.ID)
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
			qtx.DeleteOtherOwnedAddresses(ctx, db.DeleteOtherOwnedAddressesParams{Entry: orig.ID, Ids: patchedAddresses})
		} else {
			qtx.DeleteAllOwnedAddresses(ctx, orig.ID)
		}

		// Update/create addresses
		for _, address := range reqaddrs {
			insertedAddress, err := qtx.UpsertAddress(ctx, db.UpsertAddressParams{
				ID:    address.Id,
				Entry: orig.ID,
				Type:  string(address.Type),
			})
			if err != nil {
				log.Fatal(err)
			}

			// Handle address components
			if address.AddressComponents != nil {
				// Delete all existing components and insert new ones
				qtx.DeleteAllOwnedAddressComponents(ctx, insertedAddress.ID)

				// Insert new components
				for _, component := range *address.AddressComponents {
					_, err = qtx.CreateAddressComponent(ctx, db.CreateAddressComponentParams{
						Address: insertedAddress.ID,
						Seq:     component.Seq,
						Type:    string(component.Type),
						Value:   component.Value,
					})
					if err != nil {
						log.Fatal(err)
					}
				}
			}
		}
	} else if request.Body.Addresses.IsNull() {
		qtx.DeleteAllOwnedAddresses(ctx, orig.ID)
	}

	err = tx.Commit(ctx)
	if err != nil {
		log.Fatal(err)
	}

	return UpdateEntry204Response{}, nil
}

func (a ApiImpl) DeleteEntry(ctx context.Context, request DeleteEntryRequestObject) (DeleteEntryResponseObject, error) {
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
		log.Fatal(err)
	}
	return DeleteEntry204Response{}, nil
}
