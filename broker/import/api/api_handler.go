package importapi

import (
	"errors"
	"mime"
	"net/http"

	"github.com/indexdata/crosslink/broker/adapter"
	brokerapi "github.com/indexdata/crosslink/broker/api"
	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/ill_db"
	importdb "github.com/indexdata/crosslink/broker/import/db"
	importoapi "github.com/indexdata/crosslink/broker/import/oapi"
	"github.com/indexdata/crosslink/broker/import/service"
	prservice "github.com/indexdata/crosslink/broker/patron_request/service"
)

type ApiHandler struct {
	importer           service.Importer
	maxImportBodyBytes int64
}

const maxImportBodyBytes int64 = 2 << 30 // 2 GB

var _ importoapi.ServerInterface = (*ApiHandler)(nil)

func NewApiHandler(repo importdb.ImportRepo, illRepo ill_db.IllRepo, directoryAdapter adapter.DirectoryLookupAdapter, stateModels *prservice.StateModelService) ApiHandler {
	return ApiHandler{
		importer: service.NewImporter(repo, illRepo, directoryAdapter, stateModels, nil),
	}
}

func (a *ApiHandler) PostImport(w http.ResponseWriter, r *http.Request, params importoapi.PostImportParams) {
	ctx := common.CreateExtCtxWithArgs(r.Context(), &common.LoggerArgs{
		Other: map[string]string{"method": "PostImport"},
	})
	policyValue := ""
	if params.ConflictPolicy != nil {
		policyValue = string(*params.ConflictPolicy)
	}
	policy, err := importdb.ParseConflictPolicy(policyValue)
	if err != nil {
		brokerapi.AddBadRequestError(ctx, w, err)
		return
	}
	if r.Body == nil || r.Body == http.NoBody {
		brokerapi.AddBadRequestError(ctx, w, errors.New("body is required"))
		return
	}
	if !isNDJSONContentType(r.Header.Get("Content-Type")) {
		brokerapi.AddBadRequestError(ctx, w, errors.New("content type must be application/x-ndjson"))
		return
	}

	bodyLimit := a.maxImportBodyBytes
	if bodyLimit <= 0 {
		bodyLimit = maxImportBodyBytes
	}
	if r.ContentLength > bodyLimit {
		http.Error(w, "import request too large", http.StatusRequestEntityTooLarge)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, bodyLimit)
	result, err := a.importer.Import(ctx, policy, r.Body)
	if err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) || errors.Is(err, service.ErrImportRecordTooLarge) {
			http.Error(w, "import request too large", http.StatusRequestEntityTooLarge)
			return
		}
		ctx.Logger().Error("failed to read import request", "error", err)
		http.Error(w, "failed to read import request", http.StatusInternalServerError)
		return
	}
	brokerapi.WriteJsonResponse(w, result)
}

func isNDJSONContentType(contentType string) bool {
	mediaType, _, err := mime.ParseMediaType(contentType)
	return err == nil && mediaType == "application/x-ndjson"
}
