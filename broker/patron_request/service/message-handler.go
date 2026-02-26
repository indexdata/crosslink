package prservice

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/events"
	"github.com/indexdata/crosslink/broker/ill_db"
	pr_db "github.com/indexdata/crosslink/broker/patron_request/db"
	"github.com/indexdata/crosslink/iso18626"
	"github.com/indexdata/go-utils/utils"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

var SUPPLIER_PATRON_PATTERN = utils.GetEnv("SUPPLIER_PATRON_PATTERN", "%v_user")

const COMP_MESSAGE = "pr_massage_handler"
const RESHARE_ADD_LOAN_CONDITION = "#ReShareAddLoanCondition#"

type PatronRequestMessageHandler struct {
	prRepo    pr_db.PrRepo
	eventRepo events.EventRepo
	illRepo   ill_db.IllRepo
	eventBus  events.EventBus
}

func CreatePatronRequestMessageHandler(prRepo pr_db.PrRepo, eventRepo events.EventRepo, illRepo ill_db.IllRepo, eventBus events.EventBus) PatronRequestMessageHandler {
	return PatronRequestMessageHandler{
		prRepo:    prRepo,
		eventRepo: eventRepo,
		illRepo:   illRepo,
		eventBus:  eventBus,
	}
}

func (m *PatronRequestMessageHandler) HandleMessage(ctx common.ExtendedContext, msg *iso18626.ISO18626Message) (*iso18626.ISO18626Message, error) {
	ctx = ctx.WithArgs(ctx.LoggerArgs().WithComponent(COMP_MESSAGE))
	if msg == nil {
		return nil, errors.New("cannot process nil message")
	}

	pr, err := m.getPatronRequest(ctx, *msg)
	if err != nil {
		return nil, err
	}
	// Create notice with result
	status, response, err := m.handlePatronRequestMessage(ctx, msg, pr)
	eventData := events.EventData{CommonEventData: events.CommonEventData{IncomingMessage: msg, OutgoingMessage: response}}
	if err != nil {
		eventData.EventError = &events.EventError{
			Message: err.Error(),
		}
	}
	_, err = m.eventBus.CreateNotice(pr.ID, events.EventNamePatronRequestMessage, eventData, status, events.EventDomainPatronRequest)
	if err != nil {
		return nil, err
	}

	return response, err
}

func (m *PatronRequestMessageHandler) handlePatronRequestMessage(ctx common.ExtendedContext, msg *iso18626.ISO18626Message, pr pr_db.PatronRequest) (events.EventStatus, *iso18626.ISO18626Message, error) {
	if msg.SupplyingAgencyMessage != nil {
		return m.handleSupplyingAgencyMessage(ctx, *msg.SupplyingAgencyMessage, pr)
	} else if msg.RequestingAgencyMessage != nil {
		return m.handleRequestingAgencyMessage(ctx, *msg.RequestingAgencyMessage, pr)
	} else if msg.Request != nil {
		return m.handleRequestMessage(ctx, *msg.Request)
	} else {
		return events.EventStatusError, nil, errors.New("cannot process message without content")
	}
}

func (m *PatronRequestMessageHandler) getPatronRequest(ctx common.ExtendedContext, msg iso18626.ISO18626Message) (pr_db.PatronRequest, error) {
	if msg.SupplyingAgencyMessage != nil {
		return m.prRepo.GetPatronRequestById(ctx, msg.SupplyingAgencyMessage.Header.RequestingAgencyRequestId)
	} else if msg.RequestingAgencyMessage != nil {
		if msg.RequestingAgencyMessage.Header.SupplyingAgencyRequestId != "" {
			return m.prRepo.GetPatronRequestById(ctx, msg.RequestingAgencyMessage.Header.SupplyingAgencyRequestId)
		} else {
			symbol := msg.RequestingAgencyMessage.Header.SupplyingAgencyId.AgencyIdType.Text + ":" + msg.RequestingAgencyMessage.Header.SupplyingAgencyId.AgencyIdValue
			return m.prRepo.GetPatronRequestBySupplierSymbolAndRequesterReqId(ctx, symbol, msg.RequestingAgencyMessage.Header.RequestingAgencyRequestId)
		}
	} else if msg.Request != nil {
		return m.prRepo.GetPatronRequestById(ctx, msg.Request.Header.RequestingAgencyRequestId)
	} else {
		return pr_db.PatronRequest{}, errors.New("missing message")
	}
}

func (m *PatronRequestMessageHandler) handleSupplyingAgencyMessage(ctx common.ExtendedContext, sam iso18626.SupplyingAgencyMessage, pr pr_db.PatronRequest) (events.EventStatus, *iso18626.ISO18626Message, error) {
	// TODO handle notifications
	switch sam.StatusInfo.Status {
	case iso18626.TypeStatusExpectToSupply:
		pr.State = BorrowerStateSupplierLocated
		supSymbol := sam.Header.SupplyingAgencyId.AgencyIdType.Text + ":" + sam.Header.SupplyingAgencyId.AgencyIdValue
		pr.SupplierSymbol = pgtype.Text{
			String: supSymbol,
			Valid:  true,
		}
		return m.updatePatronRequestAndCreateSamResponse(ctx, pr, sam)
	case iso18626.TypeStatusWillSupply:
		if strings.Contains(sam.MessageInfo.Note, RESHARE_ADD_LOAN_CONDITION) {
			pr.State = BorrowerStateConditionPending
			// TODO Save conditions
		} else {
			pr.State = BorrowerStateWillSupply
		}
		// TODO should we check if supplier is set ? and search if not
		return m.updatePatronRequestAndCreateSamResponse(ctx, pr, sam)
	case iso18626.TypeStatusLoaned:
		err := m.saveItems(ctx, pr, sam)
		if err != nil {
			return createSAMResponse(sam, iso18626.TypeMessageStatusERROR, &iso18626.ErrorData{
				ErrorType:  iso18626.TypeErrorTypeUnrecognisedDataValue,
				ErrorValue: err.Error(),
			}, err)
		}
		pr.State = BorrowerStateShipped
		return m.updatePatronRequestAndCreateSamResponse(ctx, pr, sam)
	case iso18626.TypeStatusLoanCompleted, iso18626.TypeStatusCopyCompleted:
		pr.State = BorrowerStateCompleted
		return m.updatePatronRequestAndCreateSamResponse(ctx, pr, sam)
	case iso18626.TypeStatusUnfilled:
		pr.State = BorrowerStateUnfilled
		return m.updatePatronRequestAndCreateSamResponse(ctx, pr, sam)
	case iso18626.TypeStatusCancelled:
		pr.State = BorrowerStateCancelled
		return m.updatePatronRequestAndCreateSamResponse(ctx, pr, sam)
	}
	return createSAMResponse(sam, iso18626.TypeMessageStatusERROR, &iso18626.ErrorData{
		ErrorType:  iso18626.TypeErrorTypeBadlyFormedMessage,
		ErrorValue: "status change no allowed",
	}, errors.New("status change no allowed"))
}

func (m *PatronRequestMessageHandler) updatePatronRequestAndCreateSamResponse(ctx common.ExtendedContext, pr pr_db.PatronRequest, sam iso18626.SupplyingAgencyMessage) (events.EventStatus, *iso18626.ISO18626Message, error) {
	_, err := m.prRepo.UpdatePatronRequest(ctx, pr_db.UpdatePatronRequestParams(pr))
	if err != nil {
		return createSAMResponse(sam, iso18626.TypeMessageStatusERROR, &iso18626.ErrorData{
			ErrorType:  iso18626.TypeErrorTypeUnrecognisedDataValue,
			ErrorValue: err.Error(),
		}, err)
	}
	err = m.extractSamNotifications(ctx, pr, sam)
	if err != nil {
		return createSAMResponse(sam, iso18626.TypeMessageStatusERROR, &iso18626.ErrorData{
			ErrorType:  iso18626.TypeErrorTypeUnrecognisedDataValue,
			ErrorValue: err.Error(),
		}, err)
	}
	return createSAMResponse(sam, iso18626.TypeMessageStatusOK, nil, nil)
}

func createSAMResponse(sam iso18626.SupplyingAgencyMessage, messageStatus iso18626.TypeMessageStatus, errorData *iso18626.ErrorData, err error) (events.EventStatus, *iso18626.ISO18626Message, error) {
	eventStatus := events.EventStatusSuccess
	if messageStatus != iso18626.TypeMessageStatusOK {
		eventStatus = events.EventStatusProblem
	}
	return eventStatus, &iso18626.ISO18626Message{
			SupplyingAgencyMessageConfirmation: &iso18626.SupplyingAgencyMessageConfirmation{
				ConfirmationHeader: iso18626.ConfirmationHeader{
					SupplyingAgencyId:         &sam.Header.SupplyingAgencyId,
					RequestingAgencyId:        &sam.Header.RequestingAgencyId,
					RequestingAgencyRequestId: sam.Header.RequestingAgencyRequestId,
					MessageStatus:             messageStatus,
				},
				ReasonForMessage: &sam.MessageInfo.ReasonForMessage,
				ErrorData:        errorData,
			},
		},
		err
}

func createRequestResponse(request iso18626.Request, messageStatus iso18626.TypeMessageStatus, errorData *iso18626.ErrorData, err error) (events.EventStatus, *iso18626.ISO18626Message, error) {
	eventStatus := events.EventStatusSuccess
	if messageStatus != iso18626.TypeMessageStatusOK {
		eventStatus = events.EventStatusProblem
	}
	return eventStatus, &iso18626.ISO18626Message{
			RequestConfirmation: &iso18626.RequestConfirmation{
				ConfirmationHeader: iso18626.ConfirmationHeader{
					SupplyingAgencyId:         &request.Header.SupplyingAgencyId,
					RequestingAgencyId:        &request.Header.RequestingAgencyId,
					RequestingAgencyRequestId: request.Header.RequestingAgencyRequestId,
					MessageStatus:             messageStatus,
				},
				ErrorData: errorData,
			},
		},
		err
}

func (m *PatronRequestMessageHandler) handleRequestMessage(ctx common.ExtendedContext, request iso18626.Request) (events.EventStatus, *iso18626.ISO18626Message, error) {
	raRequestId := request.Header.RequestingAgencyRequestId
	if raRequestId == "" {
		return createRequestResponse(request, iso18626.TypeMessageStatusERROR, &iso18626.ErrorData{
			ErrorType:  iso18626.TypeErrorTypeUnrecognisedDataValue,
			ErrorValue: "missing RequestingAgencyRequestId",
		}, nil)
	}
	supplierSymbol := request.Header.SupplyingAgencyId.AgencyIdType.Text + ":" + request.Header.SupplyingAgencyId.AgencyIdValue
	requesterSymbol := request.Header.RequestingAgencyId.AgencyIdType.Text + ":" + request.Header.RequestingAgencyId.AgencyIdValue
	_, err := m.prRepo.GetPatronRequestBySupplierSymbolAndRequesterReqId(ctx, supplierSymbol, raRequestId)
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			return createRequestResponse(request, iso18626.TypeMessageStatusERROR, &iso18626.ErrorData{
				ErrorType:  iso18626.TypeErrorTypeUnrecognisedDataValue,
				ErrorValue: err.Error(),
			}, err)
		}
	} else {
		return createRequestResponse(request, iso18626.TypeMessageStatusERROR, &iso18626.ErrorData{
			ErrorType:  iso18626.TypeErrorTypeBadlyFormedMessage,
			ErrorValue: "there is already request with this id " + raRequestId,
		}, errors.New("duplicate request: there is already a request with this id "+raRequestId))
	}
	requestBytes, err := json.Marshal(request)
	if err != nil {
		return createRequestResponse(request, iso18626.TypeMessageStatusERROR, &iso18626.ErrorData{
			ErrorType:  iso18626.TypeErrorTypeUnrecognisedDataValue,
			ErrorValue: err.Error(),
		}, err)
	}
	pr, err := m.prRepo.CreatePatronRequest(ctx, pr_db.CreatePatronRequestParams{
		ID:              uuid.NewString(),
		Timestamp:       pgtype.Timestamp{Valid: true, Time: time.Now()},
		State:           LenderStateNew,
		Side:            SideLending,
		Patron:          getDbText(fmt.Sprintf(SUPPLIER_PATRON_PATTERN, request.Header.SupplyingAgencyId.AgencyIdValue)),
		RequesterSymbol: getDbText(requesterSymbol),
		IllRequest:      requestBytes,
		SupplierSymbol:  getDbText(supplierSymbol),
		RequesterReqID:  getDbText(raRequestId),
	})
	if err != nil {
		return createRequestResponse(request, iso18626.TypeMessageStatusERROR, &iso18626.ErrorData{
			ErrorType:  iso18626.TypeErrorTypeUnrecognisedDataValue,
			ErrorValue: err.Error(),
		}, err)
	}
	err = m.extractRequestNotifications(ctx, pr, request)
	if err != nil {
		return createRequestResponse(request, iso18626.TypeMessageStatusERROR, &iso18626.ErrorData{
			ErrorType:  iso18626.TypeErrorTypeUnrecognisedDataValue,
			ErrorValue: err.Error(),
		}, err)
	}
	action := LenderActionValidate
	_, err = m.eventBus.CreateTask(pr.ID, events.EventNameInvokeAction, events.EventData{CommonEventData: events.CommonEventData{Action: &action}}, events.EventDomainPatronRequest, nil)
	if err != nil {
		return createRequestResponse(request, iso18626.TypeMessageStatusERROR, &iso18626.ErrorData{
			ErrorType:  iso18626.TypeErrorTypeUnrecognisedDataValue,
			ErrorValue: err.Error(),
		}, err)
	}

	return createRequestResponse(request, iso18626.TypeMessageStatusOK, nil, nil)
}

func getDbText(value string) pgtype.Text {
	return pgtype.Text{
		Valid:  true,
		String: value,
	}
}

func (m *PatronRequestMessageHandler) handleRequestingAgencyMessage(ctx common.ExtendedContext, ram iso18626.RequestingAgencyMessage, pr pr_db.PatronRequest) (events.EventStatus, *iso18626.ISO18626Message, error) {
	switch ram.Action {
	case iso18626.TypeActionNotification,
		iso18626.TypeActionStatusRequest,
		iso18626.TypeActionRenew,
		iso18626.TypeActionShippedForward,
		iso18626.TypeActionReceived:
		return m.updatePatronRequestAndCreateRamResponse(ctx, pr, ram, &ram.Action)
	case iso18626.TypeActionCancel:
		pr.State = LenderStateCancelRequested
		return m.updatePatronRequestAndCreateRamResponse(ctx, pr, ram, &ram.Action)
	case iso18626.TypeActionShippedReturn:
		pr.State = LenderStateShippedReturn
		return m.updatePatronRequestAndCreateRamResponse(ctx, pr, ram, &ram.Action)
	}
	err := errors.New("unknown action: " + string(ram.Action))
	return createRAMResponse(ram, iso18626.TypeMessageStatusERROR, &ram.Action, &iso18626.ErrorData{
		ErrorType:  iso18626.TypeErrorTypeUnrecognisedDataValue,
		ErrorValue: err.Error(),
	}, err)
}

func createRAMResponse(ram iso18626.RequestingAgencyMessage, messageStatus iso18626.TypeMessageStatus, action *iso18626.TypeAction, errorData *iso18626.ErrorData, err error) (events.EventStatus, *iso18626.ISO18626Message, error) {
	eventStatus := events.EventStatusSuccess
	if messageStatus != iso18626.TypeMessageStatusOK {
		eventStatus = events.EventStatusProblem
	}
	return eventStatus, &iso18626.ISO18626Message{
			RequestingAgencyMessageConfirmation: &iso18626.RequestingAgencyMessageConfirmation{
				ConfirmationHeader: iso18626.ConfirmationHeader{
					SupplyingAgencyId:         &ram.Header.SupplyingAgencyId,
					RequestingAgencyId:        &ram.Header.RequestingAgencyId,
					RequestingAgencyRequestId: ram.Header.RequestingAgencyRequestId,
					MessageStatus:             messageStatus,
				},
				Action:    action,
				ErrorData: errorData,
			},
		},
		err
}

func (m *PatronRequestMessageHandler) updatePatronRequestAndCreateRamResponse(ctx common.ExtendedContext, pr pr_db.PatronRequest, ram iso18626.RequestingAgencyMessage, action *iso18626.TypeAction) (events.EventStatus, *iso18626.ISO18626Message, error) {
	_, err := m.prRepo.UpdatePatronRequest(ctx, pr_db.UpdatePatronRequestParams(pr))
	if err != nil {
		return createRAMResponse(ram, iso18626.TypeMessageStatusERROR, action, &iso18626.ErrorData{
			ErrorType:  iso18626.TypeErrorTypeUnrecognisedDataValue,
			ErrorValue: err.Error(),
		}, err)
	}
	err = m.extractRamNotifications(ctx, pr, ram)
	if err != nil {
		return createRAMResponse(ram, iso18626.TypeMessageStatusERROR, action, &iso18626.ErrorData{
			ErrorType:  iso18626.TypeErrorTypeUnrecognisedDataValue,
			ErrorValue: err.Error(),
		}, err)
	}
	return createRAMResponse(ram, iso18626.TypeMessageStatusOK, action, nil, nil)
}

func (m *PatronRequestMessageHandler) saveItems(ctx common.ExtendedContext, pr pr_db.PatronRequest, sam iso18626.SupplyingAgencyMessage) error {
	if common.SamHasItems(sam) {
		result, _, _ := common.GetItemParams(sam.MessageInfo.Note)
		for _, item := range result {
			var loopErr error
			if len(item) == 1 {
				loopErr = m.saveItem(ctx, pr.ID, item[0], item[0], nil)
			} else if len(item) == 3 {
				loopErr = m.saveItem(ctx, pr.ID, item[2], item[0], &item[1])
			} else {
				loopErr = errors.New("incorrect item param count: " + strconv.Itoa(len(item)))
			}
			if loopErr != nil {
				return loopErr
			}
		}
	}
	return nil
}

func (m *PatronRequestMessageHandler) saveItem(ctx common.ExtendedContext, prId string, id string, name string, callNumber *string) error {
	dbCallNumber := pgtype.Text{Valid: false, String: ""}
	if callNumber != nil {
		dbCallNumber = pgtype.Text{Valid: true, String: *callNumber}
	}
	_, err := m.prRepo.SaveItem(ctx, pr_db.SaveItemParams{
		ID:         uuid.NewString(),
		CreatedAt:  pgtype.Timestamp{Valid: true, Time: time.Now()},
		PrID:       prId,
		ItemID:     getDbText(id),
		Title:      getDbText(name),
		CallNumber: dbCallNumber,
		Barcode:    id, //TODO barcode generation. How to do that?
	})
	return err
}

func (m *PatronRequestMessageHandler) extractSamNotifications(ctx common.ExtendedContext, pr pr_db.PatronRequest, sam iso18626.SupplyingAgencyMessage) error {
	if sam.MessageInfo.Note != "" {
		supSymbol, reqSymbol := getSymbolsFromHeader(sam.Header)
		_, err := m.prRepo.SaveNotification(ctx, pr_db.SaveNotificationParams{
			ID:         uuid.NewString(),
			PrID:       pr.ID,
			Note:       getDbText(sam.MessageInfo.Note),
			FromSymbol: supSymbol,
			ToSymbol:   reqSymbol,
			Side:       pr.Side,
			CreatedAt: pgtype.Timestamp{
				Valid: true,
				Time:  time.Now(),
			},
		})
		if err != nil {
			return err
		}
	}
	if sam.MessageInfo.OfferedCosts != nil {
		supSymbol, reqSymbol := getSymbolsFromHeader(sam.Header)
		_, err := m.prRepo.SaveNotification(ctx, pr_db.SaveNotificationParams{
			ID:         uuid.NewString(),
			PrID:       pr.ID,
			FromSymbol: supSymbol,
			ToSymbol:   reqSymbol,
			Side:       pr.Side,
			Note:       getDbText("Offered costs"),
			Currency:   getDbText(sam.MessageInfo.OfferedCosts.CurrencyCode.Text),
			Cost: pgtype.Numeric{
				Valid: true,
				Int:   big.NewInt(int64(sam.MessageInfo.OfferedCosts.MonetaryValue.Base)),
				Exp:   utils.Must(safeConvertInt32(sam.MessageInfo.OfferedCosts.MonetaryValue.Exp)),
			},
			CreatedAt: pgtype.Timestamp{
				Valid: true,
				Time:  time.Now(),
			},
		})
		if err != nil {
			return err
		}
	}
	if sam.DeliveryInfo != nil {
		if sam.DeliveryInfo.DeliveryCosts != nil {
			supSymbol, reqSymbol := getSymbolsFromHeader(sam.Header)
			_, err := m.prRepo.SaveNotification(ctx, pr_db.SaveNotificationParams{
				ID:         uuid.NewString(),
				PrID:       pr.ID,
				FromSymbol: supSymbol,
				ToSymbol:   reqSymbol,
				Side:       pr.Side,
				Note:       getDbText("Delivery costs"),
				Currency:   getDbText(sam.DeliveryInfo.DeliveryCosts.CurrencyCode.Text),
				Cost: pgtype.Numeric{
					Valid: true,
					Int:   big.NewInt(int64(sam.DeliveryInfo.DeliveryCosts.MonetaryValue.Base)),
					Exp:   utils.Must(safeConvertInt32(sam.DeliveryInfo.DeliveryCosts.MonetaryValue.Exp)),
				},
				CreatedAt: pgtype.Timestamp{
					Valid: true,
					Time:  time.Now(),
				},
			})
			if err != nil {
				return err
			}
		}
		if sam.DeliveryInfo.LoanCondition != nil {
			supSymbol, reqSymbol := getSymbolsFromHeader(sam.Header)
			_, err := m.prRepo.SaveNotification(ctx, pr_db.SaveNotificationParams{
				ID:         uuid.NewString(),
				PrID:       pr.ID,
				FromSymbol: supSymbol,
				ToSymbol:   reqSymbol,
				Side:       pr.Side,
				Condition:  getDbText(sam.DeliveryInfo.LoanCondition.Text),
				CreatedAt: pgtype.Timestamp{
					Valid: true,
					Time:  time.Now(),
				},
			})
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func (m *PatronRequestMessageHandler) extractRamNotifications(ctx common.ExtendedContext, pr pr_db.PatronRequest, ram iso18626.RequestingAgencyMessage) error {
	if ram.Note != "" {
		supSymbol, reqSymbol := getSymbolsFromHeader(ram.Header)
		_, err := m.prRepo.SaveNotification(ctx, pr_db.SaveNotificationParams{
			ID:         uuid.NewString(),
			PrID:       pr.ID,
			Note:       getDbText(ram.Note),
			FromSymbol: reqSymbol,
			ToSymbol:   supSymbol,
			Side:       pr.Side,
			CreatedAt: pgtype.Timestamp{
				Valid: true,
				Time:  time.Now(),
			},
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func (m *PatronRequestMessageHandler) extractRequestNotifications(ctx common.ExtendedContext, pr pr_db.PatronRequest, request iso18626.Request) error {
	if request.ServiceInfo != nil && request.ServiceInfo.Note != "" {
		supSymbol, reqSymbol := getSymbolsFromHeader(request.Header)
		_, err := m.prRepo.SaveNotification(ctx, pr_db.SaveNotificationParams{
			ID:         uuid.NewString(),
			PrID:       pr.ID,
			Note:       getDbText(request.ServiceInfo.Note),
			FromSymbol: reqSymbol,
			ToSymbol:   supSymbol,
			Side:       pr.Side,
			CreatedAt: pgtype.Timestamp{
				Valid: true,
				Time:  time.Now(),
			},
		})
		if err != nil {
			return err
		}
	}
	if request.BillingInfo != nil && request.BillingInfo.MaximumCosts != nil {
		supSymbol, reqSymbol := getSymbolsFromHeader(request.Header)
		_, err := m.prRepo.SaveNotification(ctx, pr_db.SaveNotificationParams{
			ID:         uuid.NewString(),
			PrID:       pr.ID,
			Note:       getDbText("Maximum costs"),
			FromSymbol: reqSymbol,
			ToSymbol:   supSymbol,
			Side:       pr.Side,
			Currency:   getDbText(request.BillingInfo.MaximumCosts.CurrencyCode.Text),
			Cost: pgtype.Numeric{
				Valid: true,
				Int:   big.NewInt(int64(request.BillingInfo.MaximumCosts.MonetaryValue.Base)),
				Exp:   utils.Must(safeConvertInt32(request.BillingInfo.MaximumCosts.MonetaryValue.Exp)),
			},
			CreatedAt: pgtype.Timestamp{
				Valid: true,
				Time:  time.Now(),
			},
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func getSymbolsFromHeader(header iso18626.Header) (string, string) {
	return header.SupplyingAgencyId.AgencyIdType.Text + ":" + header.SupplyingAgencyId.AgencyIdValue,
		header.RequestingAgencyId.AgencyIdType.Text + ":" + header.RequestingAgencyId.AgencyIdValue
}

func safeConvertInt32(n int) (int32, error) {
	if n < math.MinInt32 || n > math.MaxInt32 {
		return 0, fmt.Errorf("integer out of range for int32: %d", n)
	}
	return int32(n), nil
}
