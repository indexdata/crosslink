package service

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/indexdata/crosslink/broker/adapter"
	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/events"
	ill_db "github.com/indexdata/crosslink/broker/ill_db"
	importdb "github.com/indexdata/crosslink/broker/import/db"
	importoapi "github.com/indexdata/crosslink/broker/import/oapi"
	pr_db "github.com/indexdata/crosslink/broker/patron_request/db"
	"github.com/indexdata/crosslink/broker/patron_request/proapi"
	prservice "github.com/indexdata/crosslink/broker/patron_request/service"
	sched_db "github.com/indexdata/crosslink/broker/scheduler/db"
	schedoapi "github.com/indexdata/crosslink/broker/scheduler/oapi"
	schedservice "github.com/indexdata/crosslink/broker/scheduler/service"
	"github.com/indexdata/crosslink/iso18626"
	"github.com/jackc/pgx/v5/pgtype"
)

const (
	importItemTypePatronRequest = "patronRequest"
	importItemTypeBatchAction   = "batchAction"
	importItemTypeTemplate      = "template"
	maxImportRecordBytes        = 1 << 20
)

var ErrImportRecordTooLarge = errors.New("import record too large")

type importStateValidator interface {
	ValidateImportState(string, proapi.StateModelServiceType, pr_db.PatronRequestSide, pr_db.PatronRequestState) (bool, error)
}

type importPeerCache interface {
	GetCachedPeersBySymbols(common.ExtendedContext, []string, adapter.DirectoryLookupAdapter) ([]ill_db.Peer, string, error)
}

type Importer struct {
	repo             importdb.ImportRepo
	peerCache        importPeerCache
	directoryAdapter adapter.DirectoryLookupAdapter
	stateValidator   importStateValidator
	clock            func() time.Time
	maxRecordBytes   int
}

type importItem struct {
	Type  string
	Owner string
	Data  json.RawMessage
}

func NewImporter(repo importdb.ImportRepo, illRepo ill_db.IllRepo, directoryAdapter adapter.DirectoryLookupAdapter, stateValidator importStateValidator, clock func() time.Time) Importer {
	return newImporter(repo, illRepo, directoryAdapter, stateValidator, clock)
}

func newImporter(repo importdb.ImportRepo, peerCache importPeerCache, directoryAdapter adapter.DirectoryLookupAdapter, stateValidator importStateValidator, clock func() time.Time) Importer {
	if clock == nil {
		clock = time.Now
	}
	return Importer{
		repo:             repo,
		peerCache:        peerCache,
		directoryAdapter: directoryAdapter,
		stateValidator:   stateValidator,
		clock:            clock,
		maxRecordBytes:   maxImportRecordBytes,
	}
}

func decodeImportItem(raw json.RawMessage) (importItem, error) {
	var envelope importItem
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return importItem{}, err
	}
	if envelope.Type == "" {
		return envelope, errors.New("type is required")
	}
	if !validImportItemType(envelope.Type) {
		return envelope, fmt.Errorf("unknown type: %s", envelope.Type)
	}
	if envelope.Data == nil {
		return envelope, errors.New("data is required")
	}
	trimmed := bytes.TrimSpace(envelope.Data)
	if len(trimmed) == 0 {
		return envelope, errors.New("data is required")
	}
	if trimmed[0] != '{' {
		return envelope, errors.New("data must be an object")
	}
	return envelope, nil
}

func (i Importer) Import(ctx common.ExtendedContext, policy importdb.ConflictPolicy, input io.Reader) (importoapi.ImportResult, error) {
	result := importoapi.ImportResult{Errors: make([]importoapi.ImportItemError, 0)}
	maxRecordBytes := i.maxRecordBytes
	if maxRecordBytes <= 0 {
		maxRecordBytes = maxImportRecordBytes
	}
	reader := bufio.NewReaderSize(input, maxRecordBytes+2)
	for line := int32(1); ; {
		raw, err := readImportRecord(reader, maxRecordBytes)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return result, err
		}
		if len(bytes.TrimSpace(raw)) == 0 {
			continue
		}

		item, err := decodeImportItem(raw)
		if err != nil {
			var syntaxError *json.SyntaxError
			if errors.As(err, &syntaxError) || errors.Is(err, io.ErrUnexpectedEOF) {
				result.Errors = append(result.Errors, importoapi.ImportItemError{Line: line, Error: err.Error()})
				break
			}
			addImportFailure(&result, line, item.Type, err, &item.Owner, nil)
			line++
			continue
		}
		identifier, repoResult, err := i.importItem(ctx, policy, item)
		if err != nil {
			addImportFailure(&result, line, item.Type, err, &item.Owner, identifier)
			line++
			continue
		}
		switch repoResult.Outcome {
		case importdb.OutcomeImported:
			addImportSuccess(&result, item.Type)
		case importdb.OutcomeSkipped:
			addImportSkipped(&result, line, item.Type, repoResult.Diagnostic, &item.Owner, identifier)
		default:
			addImportFailure(&result, line, item.Type, fmt.Errorf("repository returned unknown outcome %q", repoResult.Outcome), &item.Owner, identifier)
		}
		line++
	}
	return result, nil
}

func readImportRecord(reader *bufio.Reader, maxBytes int) (json.RawMessage, error) {
	record, err := reader.ReadSlice('\n')
	if errors.Is(err, bufio.ErrBufferFull) {
		return nil, ErrImportRecordTooLarge
	}
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	if len(record) == 0 && errors.Is(err, io.EOF) {
		return nil, io.EOF
	}
	if record[len(record)-1] == '\n' {
		record = record[:len(record)-1]
		if len(record) > 0 && record[len(record)-1] == '\r' {
			record = record[:len(record)-1]
		}
	}
	if len(record) > maxBytes {
		return nil, ErrImportRecordTooLarge
	}
	return json.RawMessage(record), nil
}

func (i Importer) importItem(ctx common.ExtendedContext, policy importdb.ConflictPolicy, item importItem) (*string, importdb.Result, error) {
	switch item.Type {
	case importItemTypePatronRequest:
		return i.importPatronRequest(ctx, policy, item.Owner, item.Data)
	case importItemTypeBatchAction:
		return i.importBatchAction(ctx, policy, item.Owner, item.Data)
	case importItemTypeTemplate:
		return i.importTemplate(ctx, policy, item.Owner, item.Data)
	default:
		return nil, importdb.Result{}, fmt.Errorf("unknown type: %s", item.Type)
	}
}

func (i Importer) importPatronRequest(ctx common.ExtendedContext, policy importdb.ConflictPolicy, owner string, data json.RawMessage) (*string, importdb.Result, error) {
	if i.repo == nil {
		return nil, importdb.Result{}, errors.New("import repository is required")
	}
	err := i.validateOwner(ctx, owner)
	if err != nil {
		return nil, importdb.Result{}, fmt.Errorf("validate owner: %w", err)
	}
	var apiBundle importoapi.ImportPatronRequestBundle
	if err = json.Unmarshal(data, &apiBundle); err != nil {
		return nil, importdb.Result{}, err
	}
	var requiredFields struct {
		IllTransaction *struct {
			IllTransactionData json.RawMessage `json:"illTransactionData"`
		} `json:"illTransaction"`
	}
	if err := json.Unmarshal(data, &requiredFields); err != nil {
		return nil, importdb.Result{}, err
	}
	if requiredFields.IllTransaction != nil {
		transactionData := bytes.TrimSpace(requiredFields.IllTransaction.IllTransactionData)
		if len(transactionData) == 0 || bytes.Equal(transactionData, []byte("null")) {
			return nil, importdb.Result{}, errors.New("illTransaction.illTransactionData is required")
		}
	}
	identifier := stringPtr(apiBundle.PatronRequest.Id)
	bundle, symbols, err := i.normalizePatronRequest(owner, apiBundle)
	if err != nil {
		return identifier, importdb.Result{}, err
	}
	if bundle.IllTransaction != nil {
		if i.peerCache == nil {
			return identifier, importdb.Result{}, errors.New("ILL peer cache is required")
		}
		peers, _, err := i.peerCache.GetCachedPeersBySymbols(ctx, symbols, i.directoryAdapter)
		if err != nil {
			return identifier, importdb.Result{}, fmt.Errorf("cache required peers: %w", err)
		}
		if len(peers) != len(symbols) {
			return identifier, importdb.Result{}, fmt.Errorf("cache required peers: expected %d peers, got %d", len(symbols), len(peers))
		}
		peerIDs := make(map[string]string, len(symbols))
		for index, symbol := range symbols {
			if peers[index].ID == "" {
				return identifier, importdb.Result{}, fmt.Errorf("cache required peers: symbol %q resolved to an empty peer ID", symbol)
			}
			peerIDs[symbol] = peers[index].ID
		}
		bundle.IllTransaction.RequesterID = pgTextFromString(peerIDs[bundle.IllTransaction.RequesterSymbol.String])
		for index := range bundle.LocatedSuppliers {
			bundle.LocatedSuppliers[index].SupplierID = peerIDs[bundle.LocatedSuppliers[index].SupplierSymbol]
		}
	}
	result, err := i.repo.ImportPatronRequest(ctx, bundle, policy)
	return identifier, result, err
}

func (i Importer) normalizePatronRequest(owner string, apiBundle importoapi.ImportPatronRequestBundle) (importdb.PatronRequestBundle, []string, error) {
	request := apiBundle.PatronRequest
	if request.Id == "" {
		return importdb.PatronRequestBundle{}, nil, errors.New("a required full migration bundle is required: patronRequest.id is required")
	}
	if request.CreatedAt.IsZero() || request.UpdatedAt.IsZero() {
		return importdb.PatronRequestBundle{}, nil, errors.New("patronRequest.createdAt and updatedAt are required")
	}
	if request.RequesterRequestId == "" || request.RequesterSymbol == "" || request.StateModel == "" || request.State == "" {
		return importdb.PatronRequestBundle{}, nil, errors.New("patronRequest requesterRequestId, requesterSymbol, stateModel, and state are required")
	}
	if apiBundle.Items == nil || apiBundle.Notifications == nil || apiBundle.LocatedSuppliers == nil {
		return importdb.PatronRequestBundle{}, nil, errors.New("items, notifications, and locatedSuppliers arrays are required")
	}
	if request.IllRequest.ServiceInfo == nil || request.IllRequest.ServiceInfo.ServiceType == "" {
		return importdb.PatronRequestBundle{}, nil, errors.New("patronRequest.illRequest.serviceInfo.serviceType is required")
	}
	serviceType := proapi.StateModelServiceType(request.IllRequest.ServiceInfo.ServiceType)
	if !serviceType.Valid() {
		return importdb.PatronRequestBundle{}, nil, fmt.Errorf("unsupported service type %q", serviceType)
	}
	var side pr_db.PatronRequestSide
	switch pr_db.PatronRequestSide(request.Side) {
	case prservice.SideBorrowing:
		side = prservice.SideBorrowing
	case prservice.SideLending:
		side = prservice.SideLending
	default:
		return importdb.PatronRequestBundle{}, nil, fmt.Errorf("unsupported patron request side %q", request.Side)
	}
	if i.stateValidator == nil {
		return importdb.PatronRequestBundle{}, nil, errors.New("state validator is required")
	}
	terminal, err := i.stateValidator.ValidateImportState(request.StateModel, serviceType, side, pr_db.PatronRequestState(request.State))
	if err != nil {
		return importdb.PatronRequestBundle{}, nil, fmt.Errorf("validate patron request state: %w", err)
	}

	illResponse := request.IllResponse
	var responseValue iso18626.SupplyingAgencyMessage
	if illResponse != nil {
		responseValue = *illResponse
	}
	bundle := importdb.PatronRequestBundle{PatronRequest: pr_db.CreatePatronRequestParams{
		ID: request.Id, CreatedAt: pgTimestamp(request.CreatedAt), UpdatedAt: pgTimestamp(request.UpdatedAt),
		IllRequest: request.IllRequest, IllResponse: responseValue,
		State: pr_db.PatronRequestState(request.State), Side: side,
		Patron: pgTextFromPtr(request.Patron), RequesterSymbol: pgTextFromString(request.RequesterSymbol),
		SupplierSymbol: pgTextFromPtr(request.SupplierSymbol), Tenant: pgTextFromString(owner),
		RequesterReqID: pgTextFromString(request.RequesterRequestId), NeedsAttention: request.NeedsAttention,
		LastAction: pgTextFromPtr(request.LastAction), LastActionOutcome: pgTextFromPtr(request.LastActionOutcome),
		LastActionResult: pgTextFromPtr(request.LastActionResult), Items: []pr_db.PrItem{}, Language: pr_db.LANGUAGE,
		TerminalState: terminal, InternalNote: pgTextFromPtr(request.InternalNote), NextReqID: pgTextFromPtr(request.NextReqId),
		PrevReqID: pgTextFromPtr(request.PrevReqId), RetryBibInfo: request.RetryBibInfo, StateModel: request.StateModel,
	}}

	seenItems := make(map[string]struct{}, len(apiBundle.Items))
	for _, item := range apiBundle.Items {
		if item.Id == "" || item.Barcode == "" || item.CreatedAt.IsZero() {
			return importdb.PatronRequestBundle{}, nil, errors.New("every item requires id, barcode, and createdAt")
		}
		if _, duplicate := seenItems[item.Id]; duplicate {
			return importdb.PatronRequestBundle{}, nil, fmt.Errorf("duplicate item id %q", item.Id)
		}
		seenItems[item.Id] = struct{}{}
		bundle.Items = append(bundle.Items, pr_db.SaveItemParams{ID: item.Id, Barcode: item.Barcode, CallNumber: pgTextFromPtr(item.CallNumber), Title: pgTextFromPtr(item.Title), ItemID: pgTextFromPtr(item.ItemId), LmsRequestID: pgTextFromPtr(item.LmsRequestId), CreatedAt: pgTimestamp(item.CreatedAt)})
	}

	seenNotifications := make(map[string]struct{}, len(apiBundle.Notifications))
	for _, notification := range apiBundle.Notifications {
		if notification.Id == "" || notification.FromSymbol == "" || notification.ToSymbol == "" || notification.CreatedAt.IsZero() {
			return importdb.PatronRequestBundle{}, nil, errors.New("every notification requires id, fromSymbol, toSymbol, and createdAt")
		}
		if !notification.Direction.Valid() || !notification.Kind.Valid() {
			return importdb.PatronRequestBundle{}, nil, fmt.Errorf("notification %q has invalid direction or kind", notification.Id)
		}
		if _, duplicate := seenNotifications[notification.Id]; duplicate {
			return importdb.PatronRequestBundle{}, nil, fmt.Errorf("duplicate notification id %q", notification.Id)
		}
		seenNotifications[notification.Id] = struct{}{}
		cost, err := pgNumericFromFloat(notification.Cost)
		if err != nil {
			return importdb.PatronRequestBundle{}, nil, fmt.Errorf("notification %q cost: %w", notification.Id, err)
		}
		bundle.Notifications = append(bundle.Notifications, pr_db.SaveNotificationParams{ID: notification.Id, FromSymbol: notification.FromSymbol, ToSymbol: notification.ToSymbol, Direction: pr_db.NotificationDirection(notification.Direction), Kind: pr_db.NotificationKind(notification.Kind), Note: pgTextFromPtr(notification.Note), Cost: cost, Currency: pgTextFromPtr(notification.Currency), Condition: pgTextFromPtr(notification.Condition), Receipt: pr_db.NotificationReceipt(valueOrEmpty(notification.Receipt)), CreatedAt: pgTimestamp(notification.CreatedAt), AcknowledgedAt: pgTimestampFromPtr(notification.AcknowledgedAt)})
	}

	if apiBundle.IllTransaction == nil {
		if len(apiBundle.LocatedSuppliers) != 0 {
			return importdb.PatronRequestBundle{}, nil, errors.New("locatedSuppliers require illTransaction")
		}
		return bundle, nil, nil
	}
	ill := apiBundle.IllTransaction
	if ill.Id == "" || ill.RequesterRequestID == "" || ill.RequesterSymbol == "" || ill.Timestamp.IsZero() {
		return importdb.PatronRequestBundle{}, nil, errors.New("illTransaction id, requesterRequestID, requesterSymbol, and timestamp are required")
	}
	if ill.RequesterRequestID != request.RequesterRequestId {
		return importdb.PatronRequestBundle{}, nil, errors.New("patronRequest and illTransaction requester request IDs must match")
	}
	bundle.IllTransaction = &ill_db.SaveIllTransactionParams{ID: ill.Id, Timestamp: pgTimestamp(ill.Timestamp), RequesterSymbol: pgTextFromString(ill.RequesterSymbol), LastRequesterAction: pgTextFromPtr(ill.LastRequesterAction), PrevRequesterAction: pgTextFromPtr(ill.PrevRequesterAction), SupplierSymbol: pgTextFromPtr(ill.SupplierSymbol), RequesterRequestID: pgTextFromString(ill.RequesterRequestID), PrevRequesterRequestID: pgTextFromPtr(ill.PrevRequesterRequestID), SupplierRequestID: pgTextFromPtr(ill.SupplierRequestID), LastSupplierStatus: pgTextFromPtr(ill.LastSupplierStatus), PrevSupplierStatus: pgTextFromPtr(ill.PrevSupplierStatus), IllTransactionData: ill.IllTransactionData}
	symbols := []string{ill.RequesterSymbol}
	seenSuppliers := make(map[string]struct{}, len(apiBundle.LocatedSuppliers))
	for _, supplier := range apiBundle.LocatedSuppliers {
		if supplier.Id == "" || supplier.SupplierSymbol == "" {
			return importdb.PatronRequestBundle{}, nil, errors.New("every located supplier requires id and supplierSymbol")
		}
		if supplier.SupplierStatus != nil && !supplier.SupplierStatus.Valid() {
			return importdb.PatronRequestBundle{}, nil, fmt.Errorf("located supplier %q has invalid status", supplier.Id)
		}
		if _, duplicate := seenSuppliers[supplier.Id]; duplicate {
			return importdb.PatronRequestBundle{}, nil, fmt.Errorf("duplicate located supplier id %q", supplier.Id)
		}
		seenSuppliers[supplier.Id] = struct{}{}
		bundle.LocatedSuppliers = append(bundle.LocatedSuppliers, ill_db.SaveLocatedSupplierParams{ID: supplier.Id, SupplierSymbol: supplier.SupplierSymbol, Ordinal: supplier.Ordinal, SupplierStatus: pgTextFromString(stringValue(supplier.SupplierStatus)), PrevAction: pgTextFromPtr(supplier.PrevAction), PrevStatus: pgTextFromPtr(supplier.PrevStatus), LastAction: pgTextFromPtr(supplier.LastAction), LastStatus: pgTextFromPtr(supplier.LastStatus), LocalID: pgTextFromPtr(supplier.LocalID), PrevReason: pgTextFromPtr(supplier.PrevReason), LastReason: pgTextFromPtr(supplier.LastReason), SupplierRequestID: pgTextFromPtr(supplier.SupplierRequestID), LocalSupplier: supplier.LocalSupplier})
		symbols = appendStableUnique(symbols, supplier.SupplierSymbol)
	}
	return bundle, symbols, nil
}

func (i Importer) importBatchAction(ctx common.ExtendedContext, policy importdb.ConflictPolicy, owner string, data json.RawMessage) (*string, importdb.Result, error) {
	if i.repo == nil {
		return nil, importdb.Result{}, errors.New("import repository is required")
	}
	err := i.validateOwner(ctx, owner)
	if err != nil {
		return nil, importdb.Result{}, fmt.Errorf("validate owner: %w", err)
	}
	var create schedoapi.CreateBatchAction
	if err = json.Unmarshal(data, &create); err != nil {
		return nil, importdb.Result{}, err
	}
	if create.Title == nil || *create.Title == "" {
		return nil, importdb.Result{}, errors.New("title must not be empty")
	}
	if !create.ActionName.Valid() {
		return create.Title, importdb.Result{}, fmt.Errorf("unknown actionName: %s", create.ActionName)
	}
	if create.Schedule == "" {
		return create.Title, importdb.Result{}, errors.New("schedule must not be empty")
	}
	if create.BatchQuery == "" {
		return create.Title, importdb.Result{}, errors.New("batchQuery must not be empty")
	}
	nextRun, err := schedservice.NextScheduleTime(create.Schedule)
	if err != nil {
		return create.Title, importdb.Result{}, err
	}
	taskID := uuid.NewString()
	paramsMap := map[string]any{}
	if create.ActionParams != nil {
		paramsMap = *create.ActionParams
	}
	now := pgtype.Timestamptz{Time: i.clock(), Valid: true}
	result, err := i.repo.ImportBatchAction(ctx, sched_db.SaveScheduledTaskParams{ID: taskID, EventName: events.EventNameInvokeBatchAction, Schedule: create.Schedule, ActionData: events.EventData{CommonEventData: events.CommonEventData{BatchActionData: &events.BatchActionData{ActionName: string(create.ActionName), Selector: create.BatchQuery, TaskId: taskID, Owner: owner}}, CustomData: paramsMap}, Title: pgTextFromPtr(create.Title), RunAt: nextRun, Status: sched_db.ScheduledTaskStatusPending, Owner: owner, CreatedAt: now, UpdatedAt: now}, policy)
	return create.Title, result, err
}

func (i Importer) importTemplate(ctx common.ExtendedContext, policy importdb.ConflictPolicy, owner string, data json.RawMessage) (*string, importdb.Result, error) {
	if i.repo == nil {
		return nil, importdb.Result{}, errors.New("import repository is required")
	}
	err := i.validateOwner(ctx, owner)
	if err != nil {
		return nil, importdb.Result{}, fmt.Errorf("validate owner: %w", err)
	}
	var create proapi.CreateTemplate
	if err = json.Unmarshal(data, &create); err != nil {
		return nil, importdb.Result{}, err
	}
	if len(create.Labels) == 0 {
		return nil, importdb.Result{}, errors.New("labels is required")
	}
	labels := strings.Join(create.Labels, ",")
	if create.Title == "" || create.Body == "" || create.Purpose == "" || create.ContentType == "" || create.Audience == nil {
		return &labels, importdb.Result{}, errors.New("title, body, purpose, contentType, and audience are required")
	}
	if !create.Purpose.Valid() {
		return &labels, importdb.Result{}, fmt.Errorf("invalid purpose: %s", create.Purpose)
	}
	if !create.ContentType.Valid() {
		return &labels, importdb.Result{}, fmt.Errorf("invalid contentType: %s", create.ContentType)
	}
	if !create.Audience.Valid() {
		return &labels, importdb.Result{}, fmt.Errorf("invalid audience: %s", *create.Audience)
	}
	now := pgtype.Timestamp{Time: i.clock(), Valid: true}
	result, err := i.repo.ImportTemplate(ctx, pr_db.SaveTemplateParams{ID: uuid.NewString(), Owner: owner, Title: create.Title, Purpose: string(create.Purpose), Subject: pgTextFromPtr(create.Subject), Body: create.Body, ContentType: string(create.ContentType), Labels: create.Labels, Audience: pgTextFromString(string(*create.Audience)), CreatedAt: now, UpdatedAt: now}, policy)
	return &labels, result, err
}

func (i Importer) validateOwner(ctx common.ExtendedContext, owner string) error {
	if owner == "" {
		return errors.New("owner is required")
	}
	peers, _, err := i.peerCache.GetCachedPeersBySymbols(ctx, []string{owner}, i.directoryAdapter)
	if err != nil {
		return err
	}
	if len(peers) == 0 {
		return errors.New("owner not found")
	}
	return nil
}

func pgTextFromPtr(value *string) pgtype.Text {
	if value == nil {
		return pgtype.Text{}
	}
	return pgTextFromString(*value)
}
func pgTextFromString(value string) pgtype.Text {
	return pgtype.Text{String: value, Valid: value != ""}
}
func pgTimestamp(value time.Time) pgtype.Timestamp {
	return pgtype.Timestamp{Time: value, Valid: !value.IsZero()}
}
func pgTimestampFromPtr(value *time.Time) pgtype.Timestamp {
	if value == nil {
		return pgtype.Timestamp{}
	}
	return pgTimestamp(*value)
}
func stringPtr(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
func valueOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
func stringValue[T ~string](value *T) string {
	if value == nil {
		return ""
	}
	return string(*value)
}

func pgNumericFromFloat(value *float64) (pgtype.Numeric, error) {
	if value == nil {
		return pgtype.Numeric{}, nil
	}
	var numeric pgtype.Numeric
	err := numeric.Scan(strconv.FormatFloat(*value, 'f', -1, 64))
	return numeric, err
}

func appendStableUnique(values []string, value string) []string {
	if value == "" {
		return values
	}
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func validImportItemType(itemType string) bool {
	switch itemType {
	case importItemTypePatronRequest, importItemTypeBatchAction, importItemTypeTemplate:
		return true
	default:
		return false
	}
}

func addImportSuccess(result *importoapi.ImportResult, itemType string) {
	switch itemType {
	case importItemTypePatronRequest:
		result.PatronRequests.Imported++
	case importItemTypeBatchAction:
		result.BatchActions.Imported++
	case importItemTypeTemplate:
		result.Templates.Imported++
	}
}

func addImportSkipped(result *importoapi.ImportResult, line int32, itemType, diagnostic string, owner, identifier *string) {
	switch itemType {
	case importItemTypePatronRequest:
		result.PatronRequests.Skipped++
	case importItemTypeBatchAction:
		result.BatchActions.Skipped++
	case importItemTypeTemplate:
		result.Templates.Skipped++
	}
	importError := importoapi.ImportItemError{Line: line, Error: diagnostic, Owner: owner, Identifier: identifier}
	if importType := importItemType(itemType); importType.Valid() {
		importError.Type = &importType
	}
	result.Errors = append(result.Errors, importError)
}

func addImportFailure(result *importoapi.ImportResult, line int32, itemType string, err error, owner, identifier *string) {
	switch itemType {
	case importItemTypePatronRequest:
		result.PatronRequests.Failed++
	case importItemTypeBatchAction:
		result.BatchActions.Failed++
	case importItemTypeTemplate:
		result.Templates.Failed++
	}
	importError := importoapi.ImportItemError{Line: line, Error: err.Error(), Owner: owner, Identifier: identifier}
	if importType := importItemType(itemType); importType.Valid() {
		importError.Type = &importType
	}
	result.Errors = append(result.Errors, importError)
}

func importItemType(itemType string) importoapi.ImportItemType {
	switch itemType {
	case importItemTypePatronRequest:
		return importoapi.ImportItemTypePatronRequest
	case importItemTypeBatchAction:
		return importoapi.ImportItemTypeBatchAction
	case importItemTypeTemplate:
		return importoapi.ImportItemTypeTemplate
	default:
		return ""
	}
}
