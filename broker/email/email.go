package email

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"html/template"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/smtp"
	"net/textproto"
	"strings"

	pr_db "github.com/indexdata/crosslink/broker/patron_request/db"
	"github.com/indexdata/crosslink/iso18626"
	"github.com/indexdata/go-utils/utils"
)

const DEFAULT_FOR_NO_VALUE = "n/a"
const DATE_LAYOUT = "2006-01-02"

// Environment variables for SMTP configuration.
var (
	SMTP_HOST     = utils.GetEnv("SMTP_HOST", "")
	SMTP_PORT     = utils.GetEnv("SMTP_PORT", "2525")
	SMTP_USERNAME = utils.GetEnv("SMTP_USERNAME", "")
	SMTP_PASSWORD = utils.GetEnv("SMTP_PASSWORD", "")
)

// Mailer is an interface over smtp.SendMail, allowing mocking in tests.
type Mailer interface {
	SendMail(addr string, a smtp.Auth, from string, to []string, msg []byte) error
}

type DefaultMailer struct{}

func (m *DefaultMailer) SendMail(addr string, a smtp.Auth, from string, to []string, msg []byte) error {
	return smtp.SendMail(addr, a, from, to, msg)
}

// EmailData carries the email payload inside an EventData.CustomData map.
type EmailData struct {
	To         []string `json:"to"`
	Subject    string   `json:"subject"`
	Body       string   `json:"body"`
	IsHTML     bool     `json:"isHtml,omitempty"`
	IncludePdf bool     `json:"includePdf,omitempty"`
}

// PdfAttach holds a PDF file to attach to the email.
type PdfAttach struct {
	Filename string
	Data     []byte
}

type EmailService interface {
	SendEmail(from string, to []string, raw []byte) error
	IsReadyToSend() bool
}
type EmailServiceImpl struct {
	mailer      Mailer
	smtpAddr    string
	smtpAuth    smtp.Auth
	readyToSend bool
}

func NewEmailService() *EmailServiceImpl {
	if SMTP_HOST == "" {
		return &EmailServiceImpl{
			readyToSend: false,
		}
	}

	var auth smtp.Auth
	if SMTP_USERNAME != "" {
		auth = smtp.PlainAuth("", SMTP_USERNAME, SMTP_PASSWORD, SMTP_HOST)
	}
	return &EmailServiceImpl{
		mailer:      &DefaultMailer{},
		smtpAddr:    fmt.Sprintf("%s:%s", SMTP_HOST, SMTP_PORT),
		smtpAuth:    auth,
		readyToSend: true,
	}
}

func (s *EmailServiceImpl) SendEmail(from string, to []string, raw []byte) error {
	if !s.readyToSend {
		return errors.New("email sender not configured")
	}
	return s.mailer.SendMail(s.smtpAddr, s.smtpAuth, from, to, raw)
}

func (s *EmailServiceImpl) IsReadyToSend() bool {
	return s.readyToSend
}

// BuildRawMessage constructs a MIME multipart/mixed raw message.
// If attachment is non-nil its bytes are included as a PDF attachment.
func BuildRawMessage(fromAddr string, data EmailData, attachment *PdfAttach) ([]byte, error) {
	if strings.ContainsAny(fromAddr, "\r\n") {
		return nil, errors.New("header injection detected in fromAddr")
	}
	if strings.ContainsAny(data.Subject, "\r\n") {
		return nil, errors.New("header injection detected in subject")
	}
	for _, addr := range data.To {
		if strings.ContainsAny(addr, "\r\n") {
			return nil, errors.New("header injection detected in to address")
		}
	}

	var buf bytes.Buffer

	// Create the multipart writer first to capture its randomly-generated
	// boundary, then reset the buffer so the top-level headers are written
	// before the first MIME part.
	mw := multipart.NewWriter(&buf)
	buf.Reset()
	buf.WriteString("From: " + fromAddr + "\r\n")
	buf.WriteString("To: " + joinAddresses(data.To) + "\r\n")
	buf.WriteString("Subject: " + mime.QEncoding.Encode("UTF-8", data.Subject) + "\r\n")
	buf.WriteString("MIME-Version: 1.0\r\n")
	buf.WriteString("Content-Type: multipart/mixed; boundary=\"" + mw.Boundary() + "\"\r\n\r\n")

	// Body part.
	bodyHeaders := make(textproto.MIMEHeader)
	if data.IsHTML {
		bodyHeaders.Set("Content-Type", "text/html; charset=UTF-8")
	} else {
		bodyHeaders.Set("Content-Type", "text/plain; charset=UTF-8")
	}
	bodyHeaders.Set("Content-Transfer-Encoding", "quoted-printable")

	bodyPart, err := mw.CreatePart(bodyHeaders)
	if err != nil {
		return nil, fmt.Errorf("create body part: %w", err)
	}
	qpw := quotedprintable.NewWriter(bodyPart)
	if _, err = qpw.Write([]byte(data.Body)); err != nil {
		return nil, fmt.Errorf("write body: %w", err)
	}
	if err = qpw.Close(); err != nil {
		return nil, fmt.Errorf("close qp writer: %w", err)
	}

	// PDF attachment part.
	if attachment != nil {
		attHeaders := make(textproto.MIMEHeader)
		attHeaders.Set("Content-Type", `application/pdf; name="`+attachment.Filename+`"`)
		attHeaders.Set("Content-Transfer-Encoding", "base64")
		attHeaders.Set("Content-Disposition", `attachment; filename="`+attachment.Filename+`"`)

		attPart, createErr := mw.CreatePart(attHeaders)
		if createErr != nil {
			return nil, fmt.Errorf("create attachment part: %w", createErr)
		}
		// Encode as base64 with RFC 2045 line wrapping (76 chars + CRLF).
		enc := base64.StdEncoding.EncodeToString(attachment.Data)
		for i := 0; i < len(enc); i += 76 {
			end := i + 76
			if end > len(enc) {
				end = len(enc)
			}
			if _, writeErr := attPart.Write([]byte(enc[i:end] + "\r\n")); writeErr != nil {
				return nil, fmt.Errorf("write attachment: %w", writeErr)
			}
		}
	}

	if err = mw.Close(); err != nil {
		return nil, fmt.Errorf("close multipart writer: %w", err)
	}
	return buf.Bytes(), nil
}

// joinAddresses joins email addresses with ", ".
func joinAddresses(addrs []string) string {
	result := ""
	for i, a := range addrs {
		if i > 0 {
			result += ", "
		}
		result += a
	}
	return result
}

// Only string fields are allowed
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
	PatronName       string
	PatronSurname    string
	PatronId         string
}

// Only string fields are allowed
type BatchEmailData struct {
	FullCount   string
	ActualCount string
	BatchQuery  string
}

func GetPullSlipData(pr pr_db.PatronRequest, notes []pr_db.Notification, conditions []pr_db.Notification, barcodeData string) PullSlipData {
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
		PatronName:       DEFAULT_FOR_NO_VALUE,
		PatronSurname:    DEFAULT_FOR_NO_VALUE,
		PatronId:         DEFAULT_FOR_NO_VALUE,
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
	if pr.IllRequest.PatronInfo != nil {
		if pr.IllRequest.PatronInfo.PatronId != "" {
			data.PatronId = pr.IllRequest.PatronInfo.PatronId
		}
		if pr.IllRequest.PatronInfo.GivenName != "" {
			data.PatronName = pr.IllRequest.PatronInfo.GivenName
		}
		if pr.IllRequest.PatronInfo.Surname != "" {
			data.PatronSurname = pr.IllRequest.PatronInfo.Surname
		}
	}
	return data
}

func RenderTemplate(data any, templateBody string) (string, error) {
	tmpl, err := template.New("pull-slip").Parse(templateBody)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
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

func GetBatchEmailData(fullCount int64, actualCount int, batchQuery string) BatchEmailData {
	return BatchEmailData{
		FullCount:   fmt.Sprintf("%d", fullCount),
		ActualCount: fmt.Sprintf("%d", actualCount),
		BatchQuery:  batchQuery,
	}
}
