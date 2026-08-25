package psservice

import (
	"bytes"
	_ "embed"
	"encoding/base64"
	"errors"
	"image/png"

	"github.com/boombuler/barcode"
	"github.com/boombuler/barcode/code128"
	"github.com/carlos7ags/folio/document"
	"github.com/carlos7ags/folio/reader"
	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/email"
	pr_db "github.com/indexdata/crosslink/broker/patron_request/db"
	"github.com/indexdata/crosslink/broker/patron_request/proapi"
	prservice "github.com/indexdata/crosslink/broker/patron_request/service"
)

type PdfService interface {
	GeneratePdfPullSlipForPrs(ctx common.ExtendedContext, prs []pr_db.PatronRequest) ([]byte, error)
}

type PdfServiceImpl struct {
	prRepo               pr_db.PrRepo
	actionMappingService prservice.ActionMappingService
}

func NewPdfService(prRepo pr_db.PrRepo) PdfService {
	return &PdfServiceImpl{
		prRepo:               prRepo,
		actionMappingService: prservice.ActionMappingService{SMService: &prservice.StateModelService{}},
	}
}

func (p *PdfServiceImpl) GeneratePdfPullSlipForPrs(ctx common.ExtendedContext, prs []pr_db.PatronRequest) ([]byte, error) {
	pdfs := []*reader.PdfReader{}
	for _, pr := range prs {
		notes, _, err := p.prRepo.GetNotificationsByPrId(ctx, pr_db.GetNotificationsByPrIdParams{Limit: 100, Offset: 0, PrID: pr.ID, Kind: string(pr_db.NotificationKindNote)})
		if err != nil {
			return []byte{}, err
		}
		conditions, _, err := p.prRepo.GetNotificationsByPrId(ctx, pr_db.GetNotificationsByPrIdParams{Limit: 100, Offset: 0, PrID: pr.ID, Kind: string(pr_db.NotificationKindCondition)})
		if err != nil {
			return []byte{}, err
		}
		pdf, err := p.GeneratePdfPullSlip(ctx, pr, notes, conditions)
		if err != nil {
			return []byte{}, err
		}
		pdfReader, err := reader.Parse(pdf)
		if err != nil {
			return []byte{}, err
		}
		pdfs = append(pdfs, pdfReader)
	}
	merged, err := reader.Merge(pdfs...)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if _, err := merged.WriteTo(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (p *PdfServiceImpl) GeneratePdfPullSlip(ctx common.ExtendedContext, pr pr_db.PatronRequest, notes []pr_db.Notification, conditions []pr_db.Notification) ([]byte, error) {
	barcodeData, err := getBarcodeBase64(pr.RequesterReqID.String)
	if err != nil {
		return nil, err
	}
	templateBody, err := p.getTemplateForPatronRequest(ctx, pr)
	if err != nil {
		return []byte{}, err
	}

	doc := document.NewDocument(document.PageSizeA4)

	data := email.GetPullSlipData(pr, notes, conditions, barcodeData)
	html, err := email.RenderPullSlipHTMLWithTemplate(data, templateBody)
	if err != nil {
		return nil, err
	}

	err = doc.AddHTML(html, nil)
	if err != nil {
		return nil, err
	}

	return doc.ToBytes()
}

func (p *PdfServiceImpl) getTemplateForPatronRequest(ctx common.ExtendedContext, pr pr_db.PatronRequest) (string, error) {
	stateModel, err := p.actionMappingService.GetStateModelForRequest(pr.IllRequest)
	if err != nil {
		return "", err
	}
	if stateModel.PullslipPdfTemplateLabel == nil || *stateModel.PullslipPdfTemplateLabel == "" {
		return "", errors.New("pullslipPdfTemplateLabel field is required")
	}
	owner := pr.RequesterSymbol
	if pr.Side == prservice.SideLending {
		owner = pr.SupplierSymbol
	}
	pdfTemplate, err := p.prRepo.GetTemplateByPurposeAudienceLabelAndOwner(ctx, pr_db.GetTemplateByPurposeAudienceLabelAndOwnerParams{
		Owner:    owner.String,
		Purpose:  string(proapi.Pullslip),
		Label:    *stateModel.PullslipPdfTemplateLabel,
		Audience: string(proapi.ModelActionParamsSendToStaff),
	})
	if err != nil {
		return "", err
	}
	if pdfTemplate.Body == "" {
		return "", errors.New("invalid pullslip pdf template, body field is required")
	}
	return pdfTemplate.Body, nil
}

// barcodeWidth calculates a suitable barcode pixel width based on the number
// of encoded characters. Code128 uses ~11 modules per character plus ~35
// modules of overhead (start, stop, check); each module is rendered at
// minModuleWidth pixels, with a minimum total width enforced.
func barcodeWidth(dataLen int) int {
	const (
		modulesPerChar  = 11
		overheadModules = 35
		minModuleWidth  = 3
		minWidth        = 200
	)
	w := (dataLen*modulesPerChar + overheadModules) * minModuleWidth
	if w < minWidth {
		return minWidth
	}
	return w
}

func getBarcodeBase64(data string) (string, error) {
	bc, err := code128.Encode(data)
	if err != nil {
		return "", err
	}
	width := barcodeWidth(len(data))
	scaled, err := barcode.Scale(bc, width, width/5)
	if err != nil {
		return "", err
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, scaled); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(buf.Bytes()), nil
}
