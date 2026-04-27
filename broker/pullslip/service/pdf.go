package psservice

import (
	"bytes"
	_ "embed"
	"encoding/base64"
	"html/template"
	"image/png"
	"os"

	"github.com/boombuler/barcode"
	"github.com/boombuler/barcode/code128"
	"github.com/carlos7ags/folio/document"
	pr_db "github.com/indexdata/crosslink/broker/patron_request/db"
)

var DEFAULT_FOR_NO_VALUE = "n/a"

type PdfService struct {
}

type PullSlipData struct {
	BorrowerName   string
	ReqId          string
	PickupLocation string
	Title          string
	Author         string
	DueDate        string
	ReturnAddress  string
	BarcodeBase64  string
}

//go:embed pull_slip_template.html
var pullSlipTemplate string

func (p *PdfService) GeneratePdfPullSlip(pr pr_db.PatronRequest) ([]byte, error) {
	barcodeData, err := getBarcodeBase64(pr.RequesterReqID.String)
	if err != nil {
		return nil, err
	}
	data := PullSlipData{
		ReqId:          pr.RequesterReqID.String,
		PickupLocation: DEFAULT_FOR_NO_VALUE,
		Title:          DEFAULT_FOR_NO_VALUE,
		Author:         DEFAULT_FOR_NO_VALUE,
		DueDate:        DEFAULT_FOR_NO_VALUE,
		ReturnAddress:  DEFAULT_FOR_NO_VALUE,
		BarcodeBase64:  barcodeData,
	}
	if pr.IllRequest.BibliographicInfo.Author != "" {
		data.Author = pr.IllRequest.BibliographicInfo.Author
	}
	if pr.IllRequest.BibliographicInfo.Title != "" {
		data.Title = pr.IllRequest.BibliographicInfo.Title
	}
	// TODO fill other fields
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
