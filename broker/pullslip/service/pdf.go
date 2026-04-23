package service

import (
	"bytes"
	"encoding/base64"
	"html/template"
	"image/png"

	"github.com/boombuler/barcode"
	"github.com/boombuler/barcode/code128"

	"github.com/carlos7ags/folio/document"
)

type SlipData struct {
	BorrowerName   string
	ReqId          string
	PickupLocation string
	Title          string
	Author         string
	DueDate        string
	ReturnAddress  string
	BarcodeBase64  string
}

var htmlContent = `<!DOCTYPE html>
<html>
<head>
	<style>
		body { font-family: Arial, sans-serif; width: 300px; font-size: 12px; }
		.frame { border: 1px soldi #000; padding: 3px }
		.box { border: 1px soldi #000; padding: 10px; margin: 8px 2px; }
		.header { font-weight: bold; font-size: 14px; margin-bottom: 5px; text-align: center; }
		.barcode { text-align: center; margin: 10px 0; }
		.section { margin-bottom: 8px; padding-left: 5px }
		.label { font-weight: bold; display: block; text-transform: uppercase; font-size: 10px; color: #555; }
		.warning { border: 2px solid #000; padding: 5px; font-weight: bold; text-align: center; margin: 10px 0; }
	</style>
</head>
<body>
	<div class="frame">
		<div class="barcode">
			<div class="header">Borrowe & More</div>
			<img src="data:image/png;base64,{{.BarcodeBase64}}" alt="Barcode">
			{{.ReqId}}
		</div>

		<div class="box">
			<div class="section">
				<span class="label">Pickup Location</span>
				{{.PickupLocation}}
			</div>
		</div>

		<div class="section">
			<span class="label">Title / Author</span>
			<strong>{{.Title}}</strong><br> by {{.Author}}
		</div>

		<div class="warning">
			DO NOT REMOVE THIS SLIP<br>
			Material on loan from a Trove Partner
		</div>

		<div class="section">
			<span class="label">Due Date</span>
			{{.DueDate}}
		</div>

		<div class="section">
			<span class="label">Return Address</span>
			{{.ReturnAddress}}
		</div>
	</div>
</body>
</html>`

func toPdf() {
	data := SlipData{
		ReqId:          "MEL-631",
		PickupLocation: "Melbourne dev tenant / Main Campus Library",
		Title:          "Big shark",
		Author:         "John Doe",
		DueDate:        "2025-12-25",
		ReturnAddress:  "38 Somebody Road, Rainbow Ville, NSW, 2600",
		BarcodeBase64:  getBarcodeBase64("MEL-631"),
	}

	doc := document.NewDocument(document.PageSizeA4)

	tmpl, _ := template.New("slip").Parse(htmlContent)
	var buf bytes.Buffer
	err := tmpl.Execute(&buf, data)
	if err != nil {
		panic(err)
	}

	err = doc.AddHTML(buf.String(), nil)
	if err != nil {
		panic(err)
	}

	err = doc.Save("loan_slip.pdf")
	if err != nil {
		panic(err)
	}
}

func getBarcodeBase64(data string) string {
	bc, _ := code128.Encode(data)
	scaled, _ := barcode.Scale(bc, 250, 50)

	var buf bytes.Buffer
	err := png.Encode(&buf, scaled)
	if err != nil {
		panic(err)
	}
	return base64.StdEncoding.EncodeToString(buf.Bytes())
}
