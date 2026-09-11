package importdb

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/indexdata/crosslink/directory/db"
	"github.com/indexdata/crosslink/directory/domain"
	"github.com/indexdata/crosslink/directory/import/model"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

const maxImportLockAttempts = 3

var errImportEntryMappingChanged = errors.New("entry symbol mapping changed while acquiring import locks")

func (r *PgImportRepo) ImportEntry(ctx context.Context, aggregate model.EntryAggregate, policy model.ConflictPolicy) (model.RepoResult, error) {
	if err := aggregate.NormalizeAndValidate(); err != nil {
		return model.RepoResult{}, err
	}
	for range maxImportLockAttempts {
		result, err := r.importEntryAttempt(ctx, aggregate, policy)
		if !errors.Is(err, errImportEntryMappingChanged) {
			return result, err
		}
		if err := ctx.Err(); err != nil {
			return model.RepoResult{}, err
		}
	}
	return model.RepoResult{}, fmt.Errorf("import entry %s: hierarchy changed repeatedly", aggregate.Key.String())
}

func (r *PgImportRepo) importEntryAttempt(ctx context.Context, aggregate model.EntryAggregate, policy model.ConflictPolicy) (model.RepoResult, error) {
	key := aggregate.Key.String()
	tx, queries, err := r.begin(ctx)
	if err != nil {
		return model.RepoResult{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := queries.LockEntryImportKey(ctx, db.LockEntryImportKeyParams{
		Authority: aggregate.Key.Authority,
		Symbol:    aggregate.Key.Symbol,
	}); err != nil {
		return model.RepoResult{}, fmt.Errorf("lock entry %s", key)
	}
	existing, lookupErr := queries.EntryBySymbol(ctx, db.EntryBySymbolParams{
		Authority: aggregate.Key.Authority,
		Symbol:    aggregate.Key.Symbol,
	})
	exists := lookupErr == nil
	if lookupErr != nil && !errors.Is(lookupErr, pgx.ErrNoRows) {
		return model.RepoResult{}, fmt.Errorf("resolve entry %s", key)
	}
	if exists && policy != model.ConflictPolicyUpdate {
		if _, err := lockEntryRows(ctx, queries, existing.ID); err != nil {
			return model.RepoResult{}, fmt.Errorf("lock entry %s: %w", key, err)
		}
		if err := lockEntryMappings(ctx, queries, entryMapping{ref: aggregate.Key, expectedOwner: &existing.ID}); err != nil {
			return model.RepoResult{}, fmt.Errorf("revalidate entry %s: %w", key, err)
		}
		return conflictResult("entry", key, policy)
	}
	if !exists && policy != model.ConflictPolicyFail && policy != model.ConflictPolicySkip && policy != model.ConflictPolicyUpdate {
		return model.RepoResult{}, fmt.Errorf("invalid conflict policy")
	}

	parent, err := resolveParent(ctx, queries, aggregate.Data.Parent)
	if err != nil {
		return model.RepoResult{}, err
	}
	lenders, err := resolveLenders(ctx, queries, aggregate.Data.ILLConfig)
	if err != nil {
		return model.RepoResult{}, err
	}
	var owner *db.Entry
	if exists {
		owner = &existing
	}
	entryIDs := entryLockIDs(owner, parent, lenders)
	lockedEntries, err := lockEntryRows(ctx, queries, entryIDs...)
	if err != nil {
		return model.RepoResult{}, fmt.Errorf("lock entry hierarchy: %w", err)
	}
	mappings := []entryMapping{{ref: aggregate.Key, expectedOwner: entryIDPointer(existing, exists)}}
	if aggregate.Data.Parent != nil {
		mappings = append(mappings, entryMapping{ref: *aggregate.Data.Parent, expectedOwner: &parent.ID})
	}
	if aggregate.Data.ILLConfig != nil {
		for index, lender := range aggregate.Data.ILLConfig.LendersOfLastResort {
			mappings = append(mappings, entryMapping{ref: lender, expectedOwner: &lenders[index].ID})
		}
	}
	if err := lockEntryMappings(ctx, queries, mappings...); err != nil {
		return model.RepoResult{}, fmt.Errorf("revalidate entry hierarchy: %w", err)
	}
	if exists {
		existing = lockedEntries[existing.ID]
	}
	var parentID *uuid.UUID
	if parent != nil {
		lockedParent := lockedEntries[parent.ID]
		if valid, reason := domain.ValidParentForType(aggregate.Data.Type, lockedParent.Type); !valid {
			return model.RepoResult{}, fmt.Errorf("invalid parent %s: %s", aggregate.Data.Parent.String(), reason)
		}
		parentID = &lockedParent.ID
	}

	if aggregate.Data.Type == "Consortium" || (exists && existing.Type == "Consortium") {
		if err := queries.LockConsortiumEntryChanges(ctx); err != nil {
			return model.RepoResult{}, fmt.Errorf("lock consortium entry changes")
		}
	}
	if aggregate.Data.Type == "Consortium" && (!exists || existing.Type != "Consortium") {
		consortium, err := queries.GetConsortialEntry(ctx)
		if err == nil && (!exists || consortium.ID != existing.ID) {
			return model.RepoResult{}, fmt.Errorf("consortium already exists")
		}
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return model.RepoResult{}, fmt.Errorf("check existing consortium")
		}
	}

	if exists {
		if err := validateEntryUpdateHierarchy(ctx, queries, existing, aggregate.Data.Type, parentID); err != nil {
			return model.RepoResult{}, err
		}
	}

	entryID, err := writeEntry(ctx, queries, existing, exists, parentID, aggregate.Data)
	if err != nil {
		return model.RepoResult{}, persistenceError("entry", key, err)
	}
	if err := replaceEntryChildren(ctx, queries, entryID, aggregate.Data); err != nil {
		return model.RepoResult{}, persistenceError("entry", key, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return model.RepoResult{}, fmt.Errorf("commit entry %s import", key)
	}
	return model.RepoResult{Outcome: model.OutcomeImported}, nil
}

func entryIDPointer(entry db.Entry, exists bool) *uuid.UUID {
	if !exists {
		return nil
	}
	return &entry.ID
}

type entryMapping struct {
	ref           model.SymbolRef
	expectedOwner *uuid.UUID
}

func lockEntryMappings(ctx context.Context, queries *db.Queries, mappings ...entryMapping) error {
	sort.Slice(mappings, func(i, j int) bool {
		if mappings[i].ref.Authority != mappings[j].ref.Authority {
			return mappings[i].ref.Authority < mappings[j].ref.Authority
		}
		return mappings[i].ref.Symbol < mappings[j].ref.Symbol
	})
	locked := make(map[string]*uuid.UUID, len(mappings))
	for _, mapping := range mappings {
		key := mapping.ref.String()
		if expectedOwner, exists := locked[key]; exists {
			if !sameEntryID(expectedOwner, mapping.expectedOwner) {
				return errImportEntryMappingChanged
			}
			continue
		}
		symbol, err := queries.SymbolByAuthorityAndSymbolForUpdate(ctx, db.SymbolByAuthorityAndSymbolForUpdateParams{
			Authority: mapping.ref.Authority,
			Symbol:    mapping.ref.Symbol,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			if mapping.expectedOwner != nil {
				return errImportEntryMappingChanged
			}
			locked[key] = nil
			continue
		}
		if err != nil {
			return err
		}
		if mapping.expectedOwner == nil || symbol.Owner != *mapping.expectedOwner {
			return errImportEntryMappingChanged
		}
		owner := symbol.Owner
		locked[key] = &owner
	}
	return nil
}

func sameEntryID(first, second *uuid.UUID) bool {
	if first == nil || second == nil {
		return first == nil && second == nil
	}
	return *first == *second
}

func resolveParent(ctx context.Context, queries *db.Queries, parent *model.SymbolRef) (*db.Entry, error) {
	if parent == nil {
		return nil, nil
	}
	entry, err := queries.EntryBySymbol(ctx, db.EntryBySymbolParams{Authority: parent.Authority, Symbol: parent.Symbol})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("parent %s does not exist", parent.String())
	}
	if err != nil {
		return nil, fmt.Errorf("resolve parent %s", parent.String())
	}
	return &entry, nil
}

func resolveLenders(ctx context.Context, queries *db.Queries, config *model.ILLConfig) ([]db.Entry, error) {
	if config == nil {
		return nil, nil
	}
	lenders := make([]db.Entry, 0, len(config.LendersOfLastResort))
	for _, lender := range config.LendersOfLastResort {
		entry, err := queries.EntryBySymbol(ctx, db.EntryBySymbolParams{Authority: lender.Authority, Symbol: lender.Symbol})
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("lender of last resort %s does not exist", lender.String())
		}
		if err != nil {
			return nil, fmt.Errorf("resolve lender of last resort %s", lender.String())
		}
		lenders = append(lenders, entry)
	}
	return lenders, nil
}

func entryLockIDs(owner, parent *db.Entry, lenders []db.Entry) []uuid.UUID {
	ids := make([]uuid.UUID, 0, 2+len(lenders))
	if owner != nil {
		ids = append(ids, owner.ID)
	}
	if parent != nil {
		ids = append(ids, parent.ID)
	}
	for _, lender := range lenders {
		ids = append(ids, lender.ID)
	}
	return orderedUniqueEntryIDs(ids...)
}

func lockEntryRows(ctx context.Context, queries *db.Queries, ids ...uuid.UUID) (map[uuid.UUID]db.Entry, error) {
	entries := make(map[uuid.UUID]db.Entry, len(ids))
	for _, id := range orderedUniqueEntryIDs(ids...) {
		entry, err := queries.EntryByIdForUpdate(ctx, id)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, errImportEntryMappingChanged
		}
		if err != nil {
			return nil, err
		}
		entries[id] = entry
	}
	return entries, nil
}

func orderedUniqueEntryIDs(ids ...uuid.UUID) []uuid.UUID {
	unique := make(map[uuid.UUID]struct{}, len(ids))
	ordered := make([]uuid.UUID, 0, len(ids))
	for _, id := range ids {
		if _, exists := unique[id]; exists {
			continue
		}
		unique[id] = struct{}{}
		ordered = append(ordered, id)
	}
	sort.Slice(ordered, func(i, j int) bool {
		return bytes.Compare(ordered[i][:], ordered[j][:]) < 0
	})
	return ordered
}

func validateEntryUpdateHierarchy(ctx context.Context, queries *db.Queries, existing db.Entry, resultingType string, parentID *uuid.UUID) error {
	if parentID != nil {
		cycle, err := queries.WouldCreateEntryCycle(ctx, db.WouldCreateEntryCycleParams{Child: existing.ID, Parent: *parentID})
		if err != nil {
			return fmt.Errorf("validate entry hierarchy")
		}
		if cycle != nil && *cycle {
			return fmt.Errorf("entry parent would create a cycle")
		}
	}
	if resultingType != existing.Type {
		children, err := queries.EntriesByParent(ctx, &existing.ID)
		if err != nil {
			return fmt.Errorf("validate entry children")
		}
		for _, child := range children {
			if valid, reason := domain.ValidParentForType(child.Type, resultingType); !valid {
				return fmt.Errorf("entry type is invalid for existing child: %s", reason)
			}
		}
	}
	return nil
}

func writeEntry(ctx context.Context, queries *db.Queries, existing db.Entry, exists bool, parentID *uuid.UUID, data model.EntryData) (uuid.UUID, error) {
	if exists {
		err := queries.UpdateEntry(ctx, db.UpdateEntryParams{
			Name: data.Name, Description: data.Description, ContactName: data.ContactName, Email: data.Email, FromEmail: data.FromEmail,
			Tenant: data.Tenant, Vendor: data.Vendor, PhoneNumber: data.PhoneNumber, TimeZone: data.TimeZone,
			OrganizationID: data.OrganizationID, Type: data.Type, Parent: parentID, LmsLocationCode: data.LMSLocationCode,
			Hrid: data.HRID, ID: existing.ID,
		})
		return existing.ID, err
	}
	created, err := queries.CreateEntry(ctx, db.CreateEntryParams{
		Name: data.Name, Description: data.Description, ContactName: data.ContactName, Email: data.Email, FromEmail: data.FromEmail,
		Tenant: data.Tenant, Vendor: data.Vendor, PhoneNumber: data.PhoneNumber, TimeZone: data.TimeZone,
		OrganizationID: data.OrganizationID, Type: data.Type, Parent: parentID, LmsLocationCode: data.LMSLocationCode,
		Hrid: data.HRID,
	})
	return created.ID, err
}

func replaceEntryChildren(ctx context.Context, queries *db.Queries, entryID uuid.UUID, data model.EntryData) error {
	if err := queries.DeleteAllOwnedSymbols(ctx, entryID); err != nil {
		return err
	}
	for _, symbol := range data.Symbols {
		if _, err := queries.UpsertSymbol(ctx, db.UpsertSymbolParams{Owner: entryID, Authority: symbol.Authority, Symbol: symbol.Symbol}); err != nil {
			return err
		}
	}
	if err := queries.DeleteAllOwnedServiceEndpoints(ctx, entryID); err != nil {
		return err
	}
	for _, endpoint := range data.Endpoints {
		if _, err := queries.UpsertServiceEndpoint(ctx, db.UpsertServiceEndpointParams{Entry: entryID, Name: endpoint.Name, Type: endpoint.Type, Address: endpoint.Address}); err != nil {
			return err
		}
	}
	if err := queries.DeleteAllOwnedAddresses(ctx, entryID); err != nil {
		return err
	}
	for _, address := range data.Addresses {
		created, err := queries.UpsertAddress(ctx, db.UpsertAddressParams{Entry: entryID, Type: address.Type})
		if err != nil {
			return err
		}
		for _, component := range address.Components {
			if _, err := queries.CreateAddressComponent(ctx, db.CreateAddressComponentParams{Address: created.ID, Seq: component.Seq, Type: component.Type, Value: component.Value}); err != nil {
				return err
			}
		}
	}
	if err := queries.DeleteClosuresByEntry(ctx, entryID); err != nil {
		return err
	}
	for _, closure := range data.Closures {
		start, _ := time.Parse(time.DateOnly, closure.StartDate)
		end, _ := time.Parse(time.DateOnly, closure.EndDate)
		if _, err := queries.CreateClosure(ctx, db.CreateClosureParams{
			Entry: entryID, StartDate: pgtype.Timestamp{Time: start, Valid: true}, EndDate: pgtype.Timestamp{Time: end, Valid: true}, Reason: closure.Reason,
		}); err != nil {
			return err
		}
	}
	return replaceEntryConfigs(ctx, queries, entryID, data)
}

func replaceEntryConfigs(ctx context.Context, queries *db.Queries, entryID uuid.UUID, data model.EntryData) error {
	if err := queries.DeleteLMSConfigByEntry(ctx, entryID); err != nil {
		return err
	}
	if data.LMSConfig != nil {
		cfg := data.LMSConfig
		var patronProfiles []byte
		if cfg.PatronProfiles != nil {
			patronProfiles, _ = json.Marshal(cfg.PatronProfiles)
		}
		if _, err := queries.UpsertLMSConfig(ctx, db.UpsertLMSConfigParams{
			Entry: &entryID, Address: cfg.Address, FromAgency: cfg.FromAgency, FromAgencyAuthentication: cfg.FromAgencyAuthentication,
			ToAgency: cfg.ToAgency, LookupUserEnabled: cfg.LookupUserEnabled, AcceptItemEnabled: cfg.AcceptItemEnabled,
			CheckinItemEnabled: cfg.CheckInItemEnabled, CheckoutItemEnabled: cfg.CheckOutItemEnabled, ItemLocation: cfg.ItemLocation,
			RequestItemRequestType: cfg.RequestItemRequestType, RequestItemScopeType: cfg.RequestItemRequestScopeType,
			RequestItemBibCode: cfg.RequestItemBibIDCode, RequestItemEnabled: cfg.RequestItemEnabled,
			RequestItemPickupLocationEnabled: cfg.RequestItemPickupLocationEnabled, RequesterPickupLocation: cfg.RequesterPickupLocation,
			SupplierPickupLocation: cfg.SupplierPickupLocation, RequesterPatronPattern: cfg.RequesterPatronPattern,
			PatronProfiles: patronProfiles,
		}); err != nil {
			return err
		}
	}
	if err := replaceCatalogConfig(ctx, queries, entryID, data.CatalogConfig); err != nil {
		return err
	}
	if err := replaceILLConfig(ctx, queries, entryID, data.ILLConfig); err != nil {
		return err
	}
	if err := queries.DeleteHoldingsPolicyByEntry(ctx, entryID); err != nil {
		return err
	}
	if data.HoldingsPolicy != nil {
		policy, err := json.Marshal(data.HoldingsPolicy)
		if err != nil {
			return err
		}
		if _, err := queries.UpsertHoldingsPolicy(ctx, db.UpsertHoldingsPolicyParams{Entry: entryID, Policy: policy}); err != nil {
			return err
		}
	}
	return nil
}

func replaceCatalogConfig(ctx context.Context, queries *db.Queries, entryID uuid.UUID, config *model.CatalogConfig) error {
	if err := queries.DeleteCatalogConfigByEntry(ctx, entryID); err != nil || config == nil {
		return err
	}
	params := db.UpsertCatalogConfigParams{Entry: &entryID, MetadataUpdateMode: config.MetadataUpdateMode}
	if config.SRU != nil {
		params.SruAddress, params.SruRecordSchema = &config.SRU.Address, config.SRU.RecordSchema
	}
	if config.Zoom != nil {
		params.ZoomAddress = &config.Zoom.Address
		if config.Zoom.Options != nil {
			params.ZoomOptions, _ = json.Marshal(config.Zoom.Options)
		}
	}
	if config.Query != nil {
		params.QueryType, params.QueryIdentifier, params.QueryIsbn, params.QueryIssn, params.QueryTitle = config.Query.Type, config.Query.Identifier, config.Query.ISBN, config.Query.ISSN, config.Query.Title
	}
	if config.HoldingsFormat != nil {
		if config.HoldingsFormat.Marc != nil {
			marc := config.HoldingsFormat.Marc
			params.HoldingsMarcCallNumberSubfield, params.HoldingsMarcItemIDSubfield = marc.CallNumberSubField, marc.ItemIDSubField
			params.HoldingsMarcLocationSubfield, params.HoldingsMarcMainField = marc.LocationSubField, marc.MainField
			params.HoldingsMarcRestrictedSubfield, params.HoldingsMarcShelvingLocationSubfield = marc.RestrictedSubField, marc.ShelvingLocationSubField
		}
		params.HoldingsMarc21plus1Enabled = boolPointer(config.HoldingsFormat.Marc21Plus1 != nil)
		params.HoldingsOpacEnabled = boolPointer(config.HoldingsFormat.OPAC != nil)
		params.HoldingsReservoirEnabled = boolPointer(config.HoldingsFormat.Reservoir != nil)
	}
	if config.MetadataFormat != nil && config.MetadataFormat.Marc21 != nil {
		marc := config.MetadataFormat.Marc21
		params.MetadataMarc21Author, params.MetadataMarc21Edition, params.MetadataMarc21Identifier = marc.Author, marc.Edition, marc.Identifier
		params.MetadataMarc21Isbn, params.MetadataMarc21Issn, params.MetadataMarc21Subtitle, params.MetadataMarc21Title = marc.ISBN, marc.ISSN, marc.Subtitle, marc.Title
	}
	_, err := queries.UpsertCatalogConfig(ctx, params)
	return err
}

func replaceILLConfig(ctx context.Context, queries *db.Queries, entryID uuid.UUID, config *model.ILLConfig) error {
	if err := queries.DeleteIllConfigByEntry(ctx, entryID); err != nil || config == nil {
		return err
	}
	lenders := make([]string, 0, len(config.LendersOfLastResort))
	for _, lender := range config.LendersOfLastResort {
		lenders = append(lenders, lender.String())
	}
	_, err := queries.UpsertIllConfig(ctx, db.UpsertIllConfigParams{
		Entry: entryID, Iso18626Url: config.ISO18626URL, Iso18626Vendor: config.ISO18626Vendor, LendersOfLastResort: lenders,
		IncludeRequestingAgencyInfo: config.IncludeRequestingAgencyInfo, IncludeSupplierInfo: config.IncludeSupplierInfo,
		IncludeReturnInfo: config.IncludeReturnInfo, IncludeVendorNote: config.IncludeVendorNote, UseOfferedCosts: config.UseOfferedCosts,
		NoteFieldSeparator: config.NoteFieldSeparator, SupplierPatronPattern: config.SupplierPatronPattern,
		DuplicateCheckWindowHours: config.DuplicateCheckWindowHours,
	})
	return err
}

func boolPointer(value bool) *bool { return &value }
