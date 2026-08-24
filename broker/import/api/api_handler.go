package importapi

import (
	"encoding/json"
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
	importer service.Importer
}

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

	result := a.importer.Import(ctx, policy, json.NewDecoder(r.Body))
	brokerapi.WriteJsonResponse(w, result)
}

func isNDJSONContentType(contentType string) bool {
	mediaType, _, err := mime.ParseMediaType(contentType)
	return err == nil && mediaType == "application/x-ndjson"
}
