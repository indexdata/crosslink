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
	"reflect"
	"strings"
	text_template "text/template"
	"text/template/parse"

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
	if pr.DueAt.Valid {
		data.DueDate = pr.DueAt.Time.Format(DATE_LAYOUT)
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

func firstTemplateLabel(labels []string) string {
	if len(labels) == 0 || labels[0] == "" {
		return "template"
	}
	return labels[0]
}

func RenderHtmlTemplate(data any, labels []string, templateBody string) (string, error) {
	label := firstTemplateLabel(labels)
	tmpl, err := template.New(label).Parse(templateBody)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func RenderTextTemplate(data any, labels []string, templateBody string) (string, error) {
	label := firstTemplateLabel(labels)
	tmpl, err := text_template.New(label).Parse(templateBody)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func ValidateHtmlTemplate(data any, labels []string, templateBody string) error {
	label := firstTemplateLabel(labels)
	tmpl, err := template.New(label).Parse(templateBody)
	if err != nil {
		return err
	}
	for _, defined := range tmpl.Templates() {
		if err := validateTemplateFields(defined.Tree.Root, data); err != nil {
			return err
		}
	}
	var buf bytes.Buffer
	return tmpl.Execute(&buf, data)
}

func ValidateTextTemplate(data any, labels []string, templateBody string) error {
	label := firstTemplateLabel(labels)
	tmpl, err := text_template.New(label).Parse(templateBody)
	if err != nil {
		return err
	}
	for _, defined := range tmpl.Templates() {
		if err := validateTemplateFields(defined.Root, data); err != nil {
			return err
		}
	}
	var buf bytes.Buffer
	return tmpl.Execute(&buf, data)
}

func validateTemplateFields(node parse.Node, data any) error {
	rootType := reflect.TypeOf(data)
	if rootType == nil {
		return errors.New("template validation data is nil")
	}

	indirect := func(dataType reflect.Type) reflect.Type {
		for dataType != nil && dataType.Kind() == reflect.Pointer {
			dataType = dataType.Elem()
		}
		return dataType
	}
	rootType = indirect(rootType)

	resolveFields := func(dataType reflect.Type, fields []string) (reflect.Type, error) {
		for _, fieldName := range fields {
			dataType = indirect(dataType)
			if dataType == nil || dataType.Kind() != reflect.Struct {
				return nil, fmt.Errorf("can't evaluate field %s in type %v", fieldName, dataType)
			}
			field, ok := dataType.FieldByName(fieldName)
			if !ok {
				return nil, fmt.Errorf("unknown field %s", fieldName)
			}
			dataType = field.Type
		}
		return dataType, nil
	}

	type templateVariables map[string]reflect.Type
	cloneVariables := func(variables templateVariables) templateVariables {
		cloned := make(templateVariables, len(variables))
		for name, dataType := range variables {
			cloned[name] = dataType
		}
		return cloned
	}

	var inferNodeType func(parse.Node, reflect.Type, templateVariables) (reflect.Type, error)
	inferNodeType = func(node parse.Node, dotType reflect.Type, variables templateVariables) (reflect.Type, error) {
		switch node := node.(type) {
		case *parse.DotNode:
			return dotType, nil
		case *parse.FieldNode:
			return resolveFields(dotType, node.Ident)
		case *parse.VariableNode:
			dataType, ok := variables[node.Ident[0]]
			if !ok {
				return nil, fmt.Errorf("unknown template variable %s", node.Ident[0])
			}
			if len(node.Ident) == 1 {
				return dataType, nil
			}
			if dataType == nil {
				return nil, fmt.Errorf("cannot validate fields on template variable %s", node.Ident[0])
			}
			return resolveFields(dataType, node.Ident[1:])
		case *parse.ChainNode:
			baseType, err := inferNodeType(node.Node, dotType, variables)
			if err != nil {
				return nil, err
			}
			return resolveFields(baseType, node.Field)
		case *parse.StringNode:
			return reflect.TypeFor[string](), nil
		case *parse.BoolNode:
			return reflect.TypeFor[bool](), nil
		case *parse.NumberNode:
			return nil, errors.New("cannot validate a numeric scoped template expression")
		default:
			return nil, fmt.Errorf("cannot validate scoped template expression %T", node)
		}
	}

	inferPipeType := func(pipe *parse.PipeNode, dotType reflect.Type, variables templateVariables) (reflect.Type, error) {
		if pipe == nil || len(pipe.Cmds) != 1 || len(pipe.Cmds[0].Args) != 1 {
			return nil, errors.New("cannot validate scoped template pipeline")
		}
		return inferNodeType(pipe.Cmds[0].Args[0], dotType, variables)
	}

	var walk func(parse.Node, reflect.Type, templateVariables) error
	walk = func(node parse.Node, dotType reflect.Type, variables templateVariables) error {
		if node == nil {
			return nil
		}
		nodeValue := reflect.ValueOf(node)
		if nodeValue.Kind() == reflect.Pointer && nodeValue.IsNil() {
			return nil
		}
		switch node := node.(type) {
		case *parse.ListNode:
			for _, child := range node.Nodes {
				if err := walk(child, dotType, variables); err != nil {
					return err
				}
			}
		case *parse.ActionNode:
			return walk(node.Pipe, dotType, variables)
		case *parse.PipeNode:
			for _, command := range node.Cmds {
				if err := walk(command, dotType, variables); err != nil {
					return err
				}
			}
			if len(node.Decl) == 1 {
				// Some pipelines (for example printf) cannot be inferred statically.
				// Retain the declaration so scalar uses remain valid; field access on
				// an unknown type is rejected later.
				variableType, _ := inferPipeType(node, dotType, variables)
				variables[node.Decl[0].Ident[0]] = variableType
			}
		case *parse.CommandNode:
			for _, argument := range node.Args {
				if err := walk(argument, dotType, variables); err != nil {
					return err
				}
			}
		case *parse.IfNode:
			branchVariables := cloneVariables(variables)
			if err := walk(node.Pipe, dotType, branchVariables); err != nil {
				return err
			}
			if err := walk(node.List, dotType, cloneVariables(branchVariables)); err != nil {
				return err
			}
			return walk(node.ElseList, dotType, cloneVariables(branchVariables))
		case *parse.RangeNode:
			branchVariables := cloneVariables(variables)
			if err := walk(node.Pipe, dotType, branchVariables); err != nil {
				return err
			}
			rangeType, err := inferPipeType(node.Pipe, dotType, variables)
			if err != nil {
				return err
			}
			rangeType = indirect(rangeType)
			if rangeType == nil {
				return errors.New("cannot validate range over an unknown type")
			}
			switch rangeType.Kind() {
			case reflect.Array, reflect.Slice, reflect.Map, reflect.Chan:
				rangeType = rangeType.Elem()
			default:
				return fmt.Errorf("cannot range over type %v", rangeType)
			}
			if len(node.Pipe.Decl) == 1 {
				branchVariables[node.Pipe.Decl[0].Ident[0]] = rangeType
			} else if len(node.Pipe.Decl) == 2 {
				branchVariables[node.Pipe.Decl[0].Ident[0]] = nil
				branchVariables[node.Pipe.Decl[1].Ident[0]] = rangeType
			}
			if err := walk(node.List, rangeType, cloneVariables(branchVariables)); err != nil {
				return err
			}
			return walk(node.ElseList, dotType, cloneVariables(branchVariables))
		case *parse.WithNode:
			branchVariables := cloneVariables(variables)
			if err := walk(node.Pipe, dotType, branchVariables); err != nil {
				return err
			}
			withType, err := inferPipeType(node.Pipe, dotType, variables)
			if err != nil {
				return err
			}
			if err := walk(node.List, withType, cloneVariables(branchVariables)); err != nil {
				return err
			}
			return walk(node.ElseList, dotType, cloneVariables(branchVariables))
		case *parse.TemplateNode:
			if err := walk(node.Pipe, dotType, variables); err != nil {
				return err
			}
			templateType, err := inferPipeType(node.Pipe, dotType, variables)
			if err != nil {
				return err
			}
			if templateType != rootType {
				return errors.New("cannot validate a template invoked with scoped data")
			}
		case *parse.FieldNode:
			_, err := resolveFields(dotType, node.Ident)
			return err
		case *parse.ChainNode:
			_, err := inferNodeType(node, dotType, variables)
			return err
		case *parse.VariableNode:
			_, err := inferNodeType(node, dotType, variables)
			return err
		}
		return nil
	}

	return walk(node, rootType, templateVariables{"$": rootType})
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
