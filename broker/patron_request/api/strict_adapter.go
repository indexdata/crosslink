package prapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"

	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/patron_request/proapi"
)

type StrictPrApiHandler struct {
	legacy *PatronRequestApiHandler
}

func NewStrictPrApiHandler(legacy *PatronRequestApiHandler) *StrictPrApiHandler {
	return &StrictPrApiHandler{legacy: legacy}
}

func (s *StrictPrApiHandler) runLegacy(ctx context.Context, body any, handler func(w http.ResponseWriter, r *http.Request)) *httptest.ResponseRecorder {
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

func decodeError(rr *httptest.ResponseRecorder) (proapi.Error, error) {
	return decodeJSON[proapi.Error](rr)
}

func unexpectedStatus(op string, rr *httptest.ResponseRecorder) error {
	return fmt.Errorf("%s: unexpected status %d, body=%q", op, rr.Code, rr.Body.String())
}

func (s *StrictPrApiHandler) GetPatronRequests(ctx context.Context, request proapi.GetPatronRequestsRequestObject) (proapi.GetPatronRequestsResponseObject, error) {
	rr := s.runLegacy(ctx, nil, func(w http.ResponseWriter, r *http.Request) {
		s.legacy.GetPatronRequests(w, r, request.Params)
	})
	switch rr.Code {
	case http.StatusOK:
		v, err := decodeJSON[proapi.PatronRequests](rr)
		return proapi.GetPatronRequests200JSONResponse(v), err
	case http.StatusBadRequest:
		v, err := decodeError(rr)
		return proapi.GetPatronRequests400JSONResponse(v), err
	case http.StatusInternalServerError:
		v, err := decodeError(rr)
		return proapi.GetPatronRequests500JSONResponse(v), err
	default:
		return nil, unexpectedStatus("GetPatronRequests", rr)
	}
}

func (s *StrictPrApiHandler) PostPatronRequests(ctx context.Context, request proapi.PostPatronRequestsRequestObject) (proapi.PostPatronRequestsResponseObject, error) {
	rr := s.runLegacy(ctx, request.Body, func(w http.ResponseWriter, r *http.Request) {
		s.legacy.PostPatronRequests(w, r, request.Params)
	})
	switch rr.Code {
	case http.StatusCreated:
		v, err := decodeJSON[proapi.PatronRequest](rr)
		return proapi.PostPatronRequests201JSONResponse{
			Body: v,
			Headers: proapi.PostPatronRequests201ResponseHeaders{
				Location: rr.Header().Get("Location"),
			},
		}, err
	case http.StatusBadRequest:
		v, err := decodeError(rr)
		return proapi.PostPatronRequests400JSONResponse(v), err
	case http.StatusInternalServerError:
		v, err := decodeError(rr)
		return proapi.PostPatronRequests500JSONResponse(v), err
	default:
		return nil, unexpectedStatus("PostPatronRequests", rr)
	}
}

func (s *StrictPrApiHandler) DeletePatronRequestsId(ctx context.Context, request proapi.DeletePatronRequestsIdRequestObject) (proapi.DeletePatronRequestsIdResponseObject, error) {
	rr := s.runLegacy(ctx, nil, func(w http.ResponseWriter, r *http.Request) {
		s.legacy.DeletePatronRequestsId(w, r, request.Id, request.Params)
	})
	switch rr.Code {
	case http.StatusNoContent:
		return proapi.DeletePatronRequestsId204Response{}, nil
	case http.StatusNotFound:
		return proapi.DeletePatronRequestsId404Response{}, nil
	case http.StatusInternalServerError:
		v, err := decodeError(rr)
		return proapi.DeletePatronRequestsId500JSONResponse(v), err
	default:
		return nil, unexpectedStatus("DeletePatronRequestsId", rr)
	}
}

func (s *StrictPrApiHandler) GetPatronRequestsId(ctx context.Context, request proapi.GetPatronRequestsIdRequestObject) (proapi.GetPatronRequestsIdResponseObject, error) {
	rr := s.runLegacy(ctx, nil, func(w http.ResponseWriter, r *http.Request) {
		s.legacy.GetPatronRequestsId(w, r, request.Id, request.Params)
	})
	switch rr.Code {
	case http.StatusOK:
		v, err := decodeJSON[proapi.PatronRequest](rr)
		return proapi.GetPatronRequestsId200JSONResponse(v), err
	case http.StatusNotFound:
		return proapi.GetPatronRequestsId404Response{}, nil
	case http.StatusInternalServerError:
		v, err := decodeError(rr)
		return proapi.GetPatronRequestsId500JSONResponse(v), err
	default:
		return nil, unexpectedStatus("GetPatronRequestsId", rr)
	}
}

func (s *StrictPrApiHandler) PostPatronRequestsIdAction(ctx context.Context, request proapi.PostPatronRequestsIdActionRequestObject) (proapi.PostPatronRequestsIdActionResponseObject, error) {
	rr := s.runLegacy(ctx, request.Body, func(w http.ResponseWriter, r *http.Request) {
		s.legacy.PostPatronRequestsIdAction(w, r, request.Id, request.Params)
	})
	switch rr.Code {
	case http.StatusOK:
		v, err := decodeJSON[proapi.ActionResult](rr)
		return proapi.PostPatronRequestsIdAction200JSONResponse(v), err
	case http.StatusBadRequest:
		v, err := decodeError(rr)
		return proapi.PostPatronRequestsIdAction400JSONResponse(v), err
	case http.StatusNotFound:
		v, err := decodeError(rr)
		return proapi.PostPatronRequestsIdAction404JSONResponse(v), err
	case http.StatusInternalServerError:
		v, err := decodeError(rr)
		return proapi.PostPatronRequestsIdAction500JSONResponse(v), err
	default:
		return nil, unexpectedStatus("PostPatronRequestsIdAction", rr)
	}
}

func (s *StrictPrApiHandler) GetPatronRequestsIdActions(ctx context.Context, request proapi.GetPatronRequestsIdActionsRequestObject) (proapi.GetPatronRequestsIdActionsResponseObject, error) {
	rr := s.runLegacy(ctx, nil, func(w http.ResponseWriter, r *http.Request) {
		s.legacy.GetPatronRequestsIdActions(w, r, request.Id, request.Params)
	})
	switch rr.Code {
	case http.StatusOK:
		v, err := decodeJSON[proapi.AllowedActions](rr)
		return proapi.GetPatronRequestsIdActions200JSONResponse(v), err
	case http.StatusNotFound:
		v, err := decodeError(rr)
		return proapi.GetPatronRequestsIdActions404JSONResponse(v), err
	case http.StatusInternalServerError:
		v, err := decodeError(rr)
		return proapi.GetPatronRequestsIdActions500JSONResponse(v), err
	default:
		return nil, unexpectedStatus("GetPatronRequestsIdActions", rr)
	}
}

func (s *StrictPrApiHandler) GetPatronRequestsIdEvents(ctx context.Context, request proapi.GetPatronRequestsIdEventsRequestObject) (proapi.GetPatronRequestsIdEventsResponseObject, error) {
	rr := s.runLegacy(ctx, nil, func(w http.ResponseWriter, r *http.Request) {
		s.legacy.GetPatronRequestsIdEvents(w, r, request.Id, request.Params)
	})
	switch rr.Code {
	case http.StatusOK:
		v, err := decodeJSON[[]proapi.Event](rr)
		return proapi.GetPatronRequestsIdEvents200JSONResponse(v), err
	case http.StatusBadRequest:
		v, err := decodeError(rr)
		return proapi.GetPatronRequestsIdEvents400JSONResponse(v), err
	case http.StatusInternalServerError:
		v, err := decodeError(rr)
		return proapi.GetPatronRequestsIdEvents500JSONResponse(v), err
	default:
		return nil, unexpectedStatus("GetPatronRequestsIdEvents", rr)
	}
}

func (s *StrictPrApiHandler) GetPatronRequestsIdItems(ctx context.Context, request proapi.GetPatronRequestsIdItemsRequestObject) (proapi.GetPatronRequestsIdItemsResponseObject, error) {
	rr := s.runLegacy(ctx, nil, func(w http.ResponseWriter, r *http.Request) {
		s.legacy.GetPatronRequestsIdItems(w, r, request.Id, request.Params)
	})
	switch rr.Code {
	case http.StatusOK:
		v, err := decodeJSON[[]proapi.PrItem](rr)
		return proapi.GetPatronRequestsIdItems200JSONResponse(v), err
	case http.StatusBadRequest:
		v, err := decodeError(rr)
		return proapi.GetPatronRequestsIdItems400JSONResponse(v), err
	case http.StatusNotFound:
		v, err := decodeError(rr)
		return proapi.GetPatronRequestsIdItems404JSONResponse(v), err
	case http.StatusInternalServerError:
		v, err := decodeError(rr)
		return proapi.GetPatronRequestsIdItems500JSONResponse(v), err
	default:
		return nil, unexpectedStatus("GetPatronRequestsIdItems", rr)
	}
}

func (s *StrictPrApiHandler) GetPatronRequestsIdNotifications(ctx context.Context, request proapi.GetPatronRequestsIdNotificationsRequestObject) (proapi.GetPatronRequestsIdNotificationsResponseObject, error) {
	rr := s.runLegacy(ctx, nil, func(w http.ResponseWriter, r *http.Request) {
		s.legacy.GetPatronRequestsIdNotifications(w, r, request.Id, request.Params)
	})
	switch rr.Code {
	case http.StatusOK:
		v, err := decodeJSON[[]proapi.PrNotification](rr)
		return proapi.GetPatronRequestsIdNotifications200JSONResponse(v), err
	case http.StatusBadRequest:
		v, err := decodeError(rr)
		return proapi.GetPatronRequestsIdNotifications400JSONResponse(v), err
	case http.StatusNotFound:
		v, err := decodeError(rr)
		return proapi.GetPatronRequestsIdNotifications404JSONResponse(v), err
	case http.StatusInternalServerError:
		v, err := decodeError(rr)
		return proapi.GetPatronRequestsIdNotifications500JSONResponse(v), err
	default:
		return nil, unexpectedStatus("GetPatronRequestsIdNotifications", rr)
	}
}

func (s *StrictPrApiHandler) GetStateModelCapabilities(ctx context.Context, request proapi.GetStateModelCapabilitiesRequestObject) (proapi.GetStateModelCapabilitiesResponseObject, error) {
	rr := s.runLegacy(ctx, nil, func(w http.ResponseWriter, r *http.Request) {
		s.legacy.GetStateModelCapabilities(w, r, request.Params)
	})
	switch rr.Code {
	case http.StatusOK:
		v, err := decodeJSON[proapi.StateModelCapabilities](rr)
		return proapi.GetStateModelCapabilities200JSONResponse(v), err
	case http.StatusInternalServerError:
		v, err := decodeError(rr)
		return proapi.GetStateModelCapabilities500JSONResponse(v), err
	default:
		return nil, unexpectedStatus("GetStateModelCapabilities", rr)
	}
}

func (s *StrictPrApiHandler) GetStateModelModelsModel(ctx context.Context, request proapi.GetStateModelModelsModelRequestObject) (proapi.GetStateModelModelsModelResponseObject, error) {
	rr := s.runLegacy(ctx, nil, func(w http.ResponseWriter, r *http.Request) {
		s.legacy.GetStateModelModelsModel(w, r, request.Model, request.Params)
	})
	switch rr.Code {
	case http.StatusOK:
		v, err := decodeJSON[proapi.StateModel](rr)
		return proapi.GetStateModelModelsModel200JSONResponse(v), err
	case http.StatusInternalServerError:
		v, err := decodeError(rr)
		return proapi.GetStateModelModelsModel500JSONResponse(v), err
	default:
		return nil, unexpectedStatus("GetStateModelModelsModel", rr)
	}
}
