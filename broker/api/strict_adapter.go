package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"

	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/oapi"
)

type StrictApiHandler struct {
	legacy *ApiHandler
}

func NewStrictApiHandler(legacy *ApiHandler) *StrictApiHandler {
	return &StrictApiHandler{legacy: legacy}
}

func (s *StrictApiHandler) runLegacy(ctx context.Context, body any, handler func(w http.ResponseWriter, r *http.Request)) *httptest.ResponseRecorder {
	req, ok := common.HTTPRequestFromContext(ctx)
	if !ok || req == nil {
		req = httptest.NewRequest(http.MethodGet, "/", nil)
	}
	req = req.Clone(ctx)
	if body != nil {
		b := []byte{}
		if v, err := json.Marshal(body); err == nil {
			b = v
		}
		req.Body = io.NopCloser(bytes.NewReader(b))
		req.ContentLength = int64(len(b))
		req.Header.Set("Content-Type", "application/json")
	}
	rr := httptest.NewRecorder()
	handler(rr, req)
	return rr
}

func decodeJSON[T any](rr *httptest.ResponseRecorder) (T, error) {
	var out T
	if rr.Body.Len() == 0 {
		return out, nil
	}
	err := json.Unmarshal(rr.Body.Bytes(), &out)
	return out, err
}

func decodeError(rr *httptest.ResponseRecorder) (oapi.Error, error) {
	return decodeJSON[oapi.Error](rr)
}

func unexpectedStatus(op string, rr *httptest.ResponseRecorder) error {
	return fmt.Errorf("%s: unexpected status %d, body=%q", op, rr.Code, rr.Body.String())
}

func (s *StrictApiHandler) Get(ctx context.Context, _ oapi.GetRequestObject) (oapi.GetResponseObject, error) {
	rr := s.runLegacy(ctx, nil, func(w http.ResponseWriter, r *http.Request) { s.legacy.Get(w, r) })
	switch rr.Code {
	case http.StatusOK:
		v, err := decodeJSON[oapi.Index](rr)
		return oapi.Get200JSONResponse(v), err
	case http.StatusInternalServerError:
		v, err := decodeError(rr)
		return oapi.Get500JSONResponse(v), err
	default:
		return nil, unexpectedStatus("Get", rr)
	}
}

func (s *StrictApiHandler) PostArchiveIllTransactions(ctx context.Context, request oapi.PostArchiveIllTransactionsRequestObject) (oapi.PostArchiveIllTransactionsResponseObject, error) {
	rr := s.runLegacy(ctx, nil, func(w http.ResponseWriter, r *http.Request) {
		s.legacy.PostArchiveIllTransactions(w, r, request.Params)
	})
	switch rr.Code {
	case http.StatusOK:
		v, err := decodeJSON[oapi.StatusMessage](rr)
		return oapi.PostArchiveIllTransactions200JSONResponse(v), err
	case http.StatusBadRequest:
		v, err := decodeError(rr)
		return oapi.PostArchiveIllTransactions400JSONResponse(v), err
	case http.StatusInternalServerError:
		v, err := decodeError(rr)
		return oapi.PostArchiveIllTransactions500JSONResponse(v), err
	default:
		return nil, unexpectedStatus("PostArchiveIllTransactions", rr)
	}
}

func (s *StrictApiHandler) GetEvents(ctx context.Context, request oapi.GetEventsRequestObject) (oapi.GetEventsResponseObject, error) {
	rr := s.runLegacy(ctx, nil, func(w http.ResponseWriter, r *http.Request) { s.legacy.GetEvents(w, r, request.Params) })
	switch rr.Code {
	case http.StatusOK:
		v, err := decodeJSON[oapi.Events](rr)
		return oapi.GetEvents200JSONResponse(v), err
	case http.StatusBadRequest:
		v, err := decodeError(rr)
		return oapi.GetEvents400JSONResponse(v), err
	case http.StatusInternalServerError:
		v, err := decodeError(rr)
		return oapi.GetEvents500JSONResponse(v), err
	default:
		return nil, unexpectedStatus("GetEvents", rr)
	}
}

func (s *StrictApiHandler) GetIllTransactions(ctx context.Context, request oapi.GetIllTransactionsRequestObject) (oapi.GetIllTransactionsResponseObject, error) {
	rr := s.runLegacy(ctx, nil, func(w http.ResponseWriter, r *http.Request) {
		s.legacy.GetIllTransactions(w, r, request.Params)
	})
	switch rr.Code {
	case http.StatusOK:
		v, err := decodeJSON[oapi.IllTransactions](rr)
		return oapi.GetIllTransactions200JSONResponse(v), err
	case http.StatusBadRequest:
		v, err := decodeError(rr)
		return oapi.GetIllTransactions400JSONResponse(v), err
	case http.StatusForbidden:
		v, err := decodeError(rr)
		return oapi.GetIllTransactions403JSONResponse(v), err
	case http.StatusInternalServerError:
		v, err := decodeError(rr)
		return oapi.GetIllTransactions500JSONResponse(v), err
	default:
		return nil, unexpectedStatus("GetIllTransactions", rr)
	}
}

func (s *StrictApiHandler) DeleteIllTransactionsId(ctx context.Context, request oapi.DeleteIllTransactionsIdRequestObject) (oapi.DeleteIllTransactionsIdResponseObject, error) {
	rr := s.runLegacy(ctx, nil, func(w http.ResponseWriter, r *http.Request) {
		s.legacy.DeleteIllTransactionsId(w, r, request.Id)
	})
	switch rr.Code {
	case http.StatusNoContent:
		return oapi.DeleteIllTransactionsId204Response{}, nil
	case http.StatusNotFound:
		v, err := decodeError(rr)
		return oapi.DeleteIllTransactionsId404JSONResponse(v), err
	case http.StatusInternalServerError:
		v, err := decodeError(rr)
		return oapi.DeleteIllTransactionsId500JSONResponse(v), err
	default:
		return nil, unexpectedStatus("DeleteIllTransactionsId", rr)
	}
}

func (s *StrictApiHandler) GetIllTransactionsId(ctx context.Context, request oapi.GetIllTransactionsIdRequestObject) (oapi.GetIllTransactionsIdResponseObject, error) {
	rr := s.runLegacy(ctx, nil, func(w http.ResponseWriter, r *http.Request) {
		s.legacy.GetIllTransactionsId(w, r, request.Id, request.Params)
	})
	switch rr.Code {
	case http.StatusOK:
		v, err := decodeJSON[oapi.IllTransaction](rr)
		return oapi.GetIllTransactionsId200JSONResponse(v), err
	case http.StatusBadRequest:
		v, err := decodeError(rr)
		return oapi.GetIllTransactionsId400JSONResponse(v), err
	case http.StatusNotFound:
		v, err := decodeError(rr)
		return oapi.GetIllTransactionsId404JSONResponse(v), err
	case http.StatusInternalServerError:
		v, err := decodeError(rr)
		return oapi.GetIllTransactionsId500JSONResponse(v), err
	default:
		return nil, unexpectedStatus("GetIllTransactionsId", rr)
	}
}

func (s *StrictApiHandler) GetLocatedSuppliers(ctx context.Context, request oapi.GetLocatedSuppliersRequestObject) (oapi.GetLocatedSuppliersResponseObject, error) {
	rr := s.runLegacy(ctx, nil, func(w http.ResponseWriter, r *http.Request) {
		s.legacy.GetLocatedSuppliers(w, r, request.Params)
	})
	switch rr.Code {
	case http.StatusOK:
		v, err := decodeJSON[oapi.LocatedSuppliers](rr)
		return oapi.GetLocatedSuppliers200JSONResponse(v), err
	case http.StatusBadRequest:
		v, err := decodeError(rr)
		return oapi.GetLocatedSuppliers400JSONResponse(v), err
	case http.StatusInternalServerError:
		v, err := decodeError(rr)
		return oapi.GetLocatedSuppliers500JSONResponse(v), err
	default:
		return nil, unexpectedStatus("GetLocatedSuppliers", rr)
	}
}

func (s *StrictApiHandler) GetPeers(ctx context.Context, request oapi.GetPeersRequestObject) (oapi.GetPeersResponseObject, error) {
	rr := s.runLegacy(ctx, nil, func(w http.ResponseWriter, r *http.Request) { s.legacy.GetPeers(w, r, request.Params) })
	switch rr.Code {
	case http.StatusOK:
		v, err := decodeJSON[oapi.Peers](rr)
		return oapi.GetPeers200JSONResponse(v), err
	case http.StatusBadRequest:
		v, err := decodeError(rr)
		return oapi.GetPeers400JSONResponse(v), err
	case http.StatusInternalServerError:
		v, err := decodeError(rr)
		return oapi.GetPeers500JSONResponse(v), err
	default:
		return nil, unexpectedStatus("GetPeers", rr)
	}
}

func (s *StrictApiHandler) PostPeers(ctx context.Context, request oapi.PostPeersRequestObject) (oapi.PostPeersResponseObject, error) {
	rr := s.runLegacy(ctx, request.Body, func(w http.ResponseWriter, r *http.Request) { s.legacy.PostPeers(w, r) })
	switch rr.Code {
	case http.StatusCreated:
		v, err := decodeJSON[oapi.Peer](rr)
		return oapi.PostPeers201JSONResponse(v), err
	case http.StatusBadRequest:
		v, err := decodeError(rr)
		return oapi.PostPeers400JSONResponse(v), err
	case http.StatusInternalServerError:
		v, err := decodeError(rr)
		return oapi.PostPeers500JSONResponse(v), err
	default:
		return nil, unexpectedStatus("PostPeers", rr)
	}
}

func (s *StrictApiHandler) DeletePeersId(ctx context.Context, request oapi.DeletePeersIdRequestObject) (oapi.DeletePeersIdResponseObject, error) {
	rr := s.runLegacy(ctx, nil, func(w http.ResponseWriter, r *http.Request) {
		s.legacy.DeletePeersId(w, r, request.Id)
	})
	switch rr.Code {
	case http.StatusNoContent:
		return oapi.DeletePeersId204Response{}, nil
	case http.StatusNotFound:
		v, err := decodeError(rr)
		return oapi.DeletePeersId404JSONResponse(v), err
	case http.StatusInternalServerError:
		v, err := decodeError(rr)
		return oapi.DeletePeersId500JSONResponse(v), err
	default:
		return nil, unexpectedStatus("DeletePeersId", rr)
	}
}

func (s *StrictApiHandler) GetPeersId(ctx context.Context, request oapi.GetPeersIdRequestObject) (oapi.GetPeersIdResponseObject, error) {
	rr := s.runLegacy(ctx, nil, func(w http.ResponseWriter, r *http.Request) {
		s.legacy.GetPeersId(w, r, request.Id)
	})
	switch rr.Code {
	case http.StatusOK:
		v, err := decodeJSON[oapi.Peer](rr)
		return oapi.GetPeersId200JSONResponse(v), err
	case http.StatusNotFound:
		v, err := decodeError(rr)
		return oapi.GetPeersId404JSONResponse(v), err
	default:
		return nil, unexpectedStatus("GetPeersId", rr)
	}
}

func (s *StrictApiHandler) PutPeersId(ctx context.Context, request oapi.PutPeersIdRequestObject) (oapi.PutPeersIdResponseObject, error) {
	rr := s.runLegacy(ctx, request.Body, func(w http.ResponseWriter, r *http.Request) {
		s.legacy.PutPeersId(w, r, request.Id)
	})
	switch rr.Code {
	case http.StatusOK:
		v, err := decodeJSON[oapi.Peer](rr)
		return oapi.PutPeersId200JSONResponse(v), err
	case http.StatusBadRequest:
		v, err := decodeError(rr)
		return oapi.PutPeersId400JSONResponse(v), err
	case http.StatusNotFound:
		v, err := decodeError(rr)
		return oapi.PutPeersId404JSONResponse(v), err
	case http.StatusInternalServerError:
		v, err := decodeError(rr)
		return oapi.PutPeersId500JSONResponse(v), err
	default:
		return nil, unexpectedStatus("PutPeersId", rr)
	}
}
