package psservice

import (
	"bytes"
	_ "embed"
	"encoding/base64"
	"html/template"
	"image/png"
	"os"
	"strings"

	"github.com/boombuler/barcode"
	"github.com/boombuler/barcode/code128"
	"github.com/carlos7ags/folio/document"
	pr_db "github.com/indexdata/crosslink/broker/patron_request/db"
	"github.com/indexdata/crosslink/iso18626"
)

const DEFAULT_FOR_NO_VALUE = "n/a"
const DATE_LAYOUT = "2006-01-02"

type PdfService struct {
}

type PullSlipData struct {
	BorrowerName     string
	ReqId            string
	PickupLocation   string
	Title            string
	Author           string
	DueDate          string
	ReturnAddress    string
	BarcodeBase64    string
	ServiceType      string
	ServiceLevel     string
	SystemIdentifier string
	Publisher        string
	Volume           string
	Issue            string
	Pages            string
	StaffNotes       string
	CallNumber       string
	LoanConditions   string
}

//go:embed pull_slip_template.html
var pullSlipTemplate string

func (p *PdfService) GeneratePdfPullSlip(pr pr_db.PatronRequest, notes []pr_db.Notification, conditions []pr_db.Notification) ([]byte, error) {
	barcodeData, err := getBarcodeBase64(pr.RequesterReqID.String)
	if err != nil {
		return nil, err
	}
	data := PullSlipData{
		ReqId:            pr.RequesterReqID.String,
		PickupLocation:   getPickupLocation(pr),
		Title:            DEFAULT_FOR_NO_VALUE,
		Author:           DEFAULT_FOR_NO_VALUE,
		DueDate:          DEFAULT_FOR_NO_VALUE,
		ReturnAddress:    DEFAULT_FOR_NO_VALUE,
		BarcodeBase64:    barcodeData,
		ServiceType:      DEFAULT_FOR_NO_VALUE,
		ServiceLevel:     DEFAULT_FOR_NO_VALUE,
		SystemIdentifier: DEFAULT_FOR_NO_VALUE,
		Publisher:        DEFAULT_FOR_NO_VALUE,
		Volume:           DEFAULT_FOR_NO_VALUE,
		Issue:            DEFAULT_FOR_NO_VALUE,
		Pages:            DEFAULT_FOR_NO_VALUE,
		StaffNotes:       getStaffNotes(notes),
		CallNumber:       getCallNumber(pr),
		LoanConditions:   getLoanConditions(conditions),
	}
	if pr.IllRequest.BibliographicInfo.Author != "" {
		data.Author = pr.IllRequest.BibliographicInfo.Author
	}
	if pr.IllRequest.BibliographicInfo.Title != "" {
		data.Title = pr.IllRequest.BibliographicInfo.Title
	}
	if pr.IllRequest.BibliographicInfo.Volume != "" {
		data.Volume = pr.IllRequest.BibliographicInfo.Volume
	}
	if pr.IllRequest.BibliographicInfo.Issue != "" {
		data.Issue = pr.IllRequest.BibliographicInfo.Issue
	}
	if pr.IllRequest.BibliographicInfo.EstimatedNoPages != "" {
		data.Pages = pr.IllRequest.BibliographicInfo.EstimatedNoPages
	}
	if pr.IllRequest.BibliographicInfo.SupplierUniqueRecordId != "" {
		data.SystemIdentifier = pr.IllRequest.BibliographicInfo.SupplierUniqueRecordId
	}
	if pr.IllRequest.PublicationInfo != nil && pr.IllRequest.PublicationInfo.Publisher != "" {
		data.Publisher = pr.IllRequest.PublicationInfo.Publisher
	}
	if pr.IllResponse.StatusInfo.DueDate != nil {
		data.DueDate = pr.IllResponse.StatusInfo.DueDate.Format(DATE_LAYOUT)
	}
	if pr.IllResponse.ReturnInfo != nil && pr.IllResponse.ReturnInfo.PhysicalAddress != nil {
		data.ReturnAddress = formatPhysicalAddress(pr.IllResponse.ReturnInfo.PhysicalAddress)
	}
	if pr.IllRequest.ServiceInfo != nil {
		if pr.IllRequest.ServiceInfo.ServiceLevel != nil && pr.IllRequest.ServiceInfo.ServiceLevel.Text != "" {
			data.ServiceLevel = pr.IllRequest.ServiceInfo.ServiceLevel.Text
		}
		if pr.IllRequest.ServiceInfo.ServiceType != "" {
			data.ServiceType = string(pr.IllRequest.ServiceInfo.ServiceType)
		}
	}
	doc := document.NewDocument(document.PageSizeA4)

	html, err := renderPullSlipHTML(data)
	if err != nil {
		return nil, err
	}

	err = doc.AddHTML(html, nil)
	if err != nil {
		return nil, err
	}

	tmp, err := os.CreateTemp("", "pull-slip-*.pdf")
	if err != nil {
		return nil, err
	}
	tmpPath := tmp.Name()
	_ = tmp.Close()
	defer os.Remove(tmpPath)

	if err := doc.Save(tmpPath); err != nil {
		return nil, err
	}

	return os.ReadFile(tmpPath)
}

func renderPullSlipHTML(data PullSlipData) (string, error) {
	tmpl, err := template.New("pull-slip").Parse(pullSlipTemplate)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
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

func formatPhysicalAddress(a *iso18626.PhysicalAddress) string {
	parts := []string{}
	if a.Line1 != "" {
		parts = append(parts, a.Line1)
	}
	if a.Line2 != "" {
		parts = append(parts, a.Line2)
	}
	if a.Locality != "" {
		parts = append(parts, a.Locality)
	}
	if a.PostalCode != "" {
		parts = append(parts, a.PostalCode)
	}
	if a.Region != nil && a.Region.Text != "" {
		parts = append(parts, a.Region.Text)
	}
	if a.Country != nil && a.Country.Text != "" {
		parts = append(parts, a.Country.Text)
	}
	return strings.Join(parts, ", ")
}

func getStaffNotes(noteList []pr_db.Notification) string {
	noteStrings := []string{}
	for _, note := range noteList {
		if note.Note.Valid {
			noteStrings = append(noteStrings, note.Note.String)
		}
	}
	notes := strings.Join(noteStrings, "\n")
	if notes == "" {
		return DEFAULT_FOR_NO_VALUE
	}
	return notes
}

func getLoanConditions(conditionList []pr_db.Notification) string {
	conditionStrings := []string{}
	for _, note := range conditionList {
		if note.Condition.Valid {
			conditionStrings = append(conditionStrings, note.Condition.String)
		}
	}
	conditions := strings.Join(conditionStrings, "\n")
	if conditions == "" {
		return DEFAULT_FOR_NO_VALUE
	}
	return conditions
}

func getCallNumber(request pr_db.PatronRequest) string {
	callNumberStrings := []string{}
	for _, item := range request.Items {
		if item.CallNumber != nil && *item.CallNumber != "" {
			callNumberStrings = append(callNumberStrings, *item.CallNumber)
		}
	}
	callNumber := strings.Join(callNumberStrings, ", ")
	if callNumber == "" {
		return DEFAULT_FOR_NO_VALUE
	}
	return callNumber
}

func getPickupLocation(request pr_db.PatronRequest) string {
	if len(request.IllRequest.RequestedDeliveryInfo) > 0 && request.IllRequest.RequestedDeliveryInfo[0].Address != nil {
		address := *request.IllRequest.RequestedDeliveryInfo[0].Address
		if address.PhysicalAddress != nil {
			return formatPhysicalAddress(address.PhysicalAddress)
		} else if address.ElectronicAddress != nil && address.ElectronicAddress.ElectronicAddressData != "" {
			return address.ElectronicAddress.ElectronicAddressData
		}
	}
	return DEFAULT_FOR_NO_VALUE
}
