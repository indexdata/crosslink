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
	"net/url"
	"reflect"
	"strings"
	text_template "text/template"
	"text/template/parse"

	pr_db "github.com/indexdata/crosslink/broker/patron_request/db"
	"github.com/indexdata/crosslink/iso18626"
	"github.com/indexdata/go-utils/utils"
	"github.com/jackc/pgx/v5/pgtype"
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
	ReqId              string
	CancellationReason string
	PickupLocation     string
	PickupURL          string
	NeededBy           string
	Title              string
	TitleOfComponent   string
	Author             string
	AuthorOfComponent  string
	DueDate            string
	ReturnAddress      string
	BarcodeBase64      string
	ServiceType        string
	ServiceLevel       string
	SystemIdentifier   string
	Publisher          string
	MaterialType       string
	Volume             string
	Issue              string
	Pages              string
	StaffNotes         string
	CallNumber         string
	Location           string
	ShelvingLocation   string
	LoanConditions     string
	PatronGivenName    string
	PatronName         string
	PatronSurname      string
	PatronId           string
	PatronProfile      string
}

// Only string fields are allowed
type BatchEmailData struct {
	FullCount   string
	ActualCount string
	BatchQuery  string
}

func GetPullSlipData(pr pr_db.PatronRequest, notes []pr_db.Notification, conditions []pr_db.Notification, barcodeData string) PullSlipData {
	data := PullSlipData{
		ReqId:              pr.RequesterReqID.String,
		CancellationReason: textValue(pr.CancellationReason, DEFAULT_FOR_NO_VALUE),
		PickupLocation:     getPickupLocation(pr),
		PickupURL:          getPickupURL(pr),
		NeededBy:           DEFAULT_FOR_NO_VALUE,
		Title:              DEFAULT_FOR_NO_VALUE,
		TitleOfComponent:   DEFAULT_FOR_NO_VALUE,
		Author:             DEFAULT_FOR_NO_VALUE,
		AuthorOfComponent:  DEFAULT_FOR_NO_VALUE,
		DueDate:            DEFAULT_FOR_NO_VALUE,
		ReturnAddress:      DEFAULT_FOR_NO_VALUE,
		BarcodeBase64:      barcodeData,
		ServiceType:        DEFAULT_FOR_NO_VALUE,
		ServiceLevel:       DEFAULT_FOR_NO_VALUE,
		SystemIdentifier:   DEFAULT_FOR_NO_VALUE,
		Publisher:          DEFAULT_FOR_NO_VALUE,
		MaterialType:       DEFAULT_FOR_NO_VALUE,
		Volume:             DEFAULT_FOR_NO_VALUE,
		Issue:              DEFAULT_FOR_NO_VALUE,
		Pages:              DEFAULT_FOR_NO_VALUE,
		StaffNotes:         getStaffNotes(notes),
		CallNumber:         getCallNumber(pr),
		Location:           getItemLocation(pr, func(item pr_db.PrItem) *string { return item.Location }),
		ShelvingLocation:   getItemLocation(pr, func(item pr_db.PrItem) *string { return item.ShelvingLocation }),
		LoanConditions:     getLoanConditions(conditions),
		PatronGivenName:    DEFAULT_FOR_NO_VALUE,
		PatronName:         DEFAULT_FOR_NO_VALUE,
		PatronSurname:      DEFAULT_FOR_NO_VALUE,
		PatronId:           DEFAULT_FOR_NO_VALUE,
		PatronProfile:      DEFAULT_FOR_NO_VALUE,
	}
	if pr.IllRequest.BibliographicInfo.Author != "" {
		data.Author = pr.IllRequest.BibliographicInfo.Author
	}
	if pr.IllRequest.BibliographicInfo.Title != "" {
		data.Title = pr.IllRequest.BibliographicInfo.Title
	}
	if pr.IllRequest.BibliographicInfo.TitleOfComponent != "" {
		data.TitleOfComponent = pr.IllRequest.BibliographicInfo.TitleOfComponent
	}
	if pr.IllRequest.BibliographicInfo.AuthorOfComponent != "" {
		data.AuthorOfComponent = pr.IllRequest.BibliographicInfo.AuthorOfComponent
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
	if pr.IllRequest.PublicationInfo != nil && pr.IllRequest.PublicationInfo.PublicationType != nil && pr.IllRequest.PublicationInfo.PublicationType.Text != "" {
		data.MaterialType = pr.IllRequest.PublicationInfo.PublicationType.Text
	}
	if pr.DueAt.Valid {
		data.DueDate = pr.DueAt.Time.Format(DATE_LAYOUT)
	}
	if pr.IllResponse.ReturnInfo != nil && pr.IllResponse.ReturnInfo.PhysicalAddress != nil {
		data.ReturnAddress = formatPhysicalAddress(pr.IllResponse.ReturnInfo.PhysicalAddress)
	}
	if pr.IllRequest.ServiceInfo != nil {
		if pr.IllRequest.ServiceInfo.NeedBeforeDate != nil && !pr.IllRequest.ServiceInfo.NeedBeforeDate.IsZero() {
			data.NeededBy = pr.IllRequest.ServiceInfo.NeedBeforeDate.Format(DATE_LAYOUT)
		}
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
			data.PatronGivenName = pr.IllRequest.PatronInfo.GivenName
			data.PatronName = pr.IllRequest.PatronInfo.GivenName
		}
		if pr.IllRequest.PatronInfo.Surname != "" {
			data.PatronSurname = pr.IllRequest.PatronInfo.Surname
		}
		if pr.IllRequest.PatronInfo.PatronType != nil && pr.IllRequest.PatronInfo.PatronType.Text != "" {
			data.PatronProfile = pr.IllRequest.PatronInfo.PatronType.Text
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

	isInteger := func(dataType reflect.Type) bool {
		if dataType == nil {
			return false
		}
		switch dataType.Kind() {
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
			reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
			return true
		default:
			return false
		}
	}

	isAssignable := func(source reflect.Type, target reflect.Type) bool {
		if source == nil || target == nil {
			return false
		}
		if source.AssignableTo(target) {
			return true
		}
		return isInteger(source) && isInteger(target) && source.ConvertibleTo(target)
	}

	type templateNil struct{}
	templateNilType := reflect.TypeFor[templateNil]()
	integerLiteral := func(node parse.Node) (int64, bool) {
		number, ok := node.(*parse.NumberNode)
		if !ok || !number.IsInt {
			return 0, false
		}
		return number.Int64, true
	}

	var inferNodeType func(parse.Node, reflect.Type, templateVariables) (reflect.Type, error)
	var inferPipeType func(*parse.PipeNode, reflect.Type, templateVariables) (reflect.Type, error)
	var inferCommandType func(*parse.CommandNode, reflect.Type, bool, reflect.Type, templateVariables) (reflect.Type, error)

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
			isRuneInt := len(node.Text) > 0 && node.Text[0] == '\''
			isHexInt := len(node.Text) > 2 && node.Text[0] == '0' &&
				(node.Text[1] == 'x' || node.Text[1] == 'X') && !strings.ContainsAny(node.Text, "pP")
			switch {
			case node.IsComplex:
				return reflect.TypeFor[complex128](), nil
			case node.IsFloat && !isHexInt && !isRuneInt && strings.ContainsAny(node.Text, ".eEpP"):
				return reflect.TypeFor[float64](), nil
			case node.IsInt:
				value := int(node.Int64)
				if int64(value) != node.Int64 {
					return nil, fmt.Errorf("%s overflows int", node.Text)
				}
				return reflect.TypeFor[int](), nil
			case node.IsUint:
				return nil, fmt.Errorf("%s overflows int", node.Text)
			default:
				return nil, errors.New("cannot infer template number type")
			}
		case *parse.NilNode:
			return templateNilType, nil
		case *parse.PipeNode:
			return inferPipeType(node, dotType, variables)
		default:
			return nil, fmt.Errorf("cannot validate scoped template expression %T", node)
		}
	}

	inferCommandType = func(command *parse.CommandNode, pipedType reflect.Type, hasPipedValue bool, dotType reflect.Type, variables templateVariables) (reflect.Type, error) {
		if command == nil || len(command.Args) == 0 {
			return nil, errors.New("cannot validate empty template command")
		}

		identifier, isBuiltin := command.Args[0].(*parse.IdentifierNode)
		if !isBuiltin {
			if hasPipedValue || len(command.Args) != 1 {
				return nil, errors.New("cannot validate a dynamic template command")
			}
			if _, isNil := command.Args[0].(*parse.NilNode); isNil {
				return nil, errors.New("nil is not a template command")
			}
			return inferNodeType(command.Args[0], dotType, variables)
		}

		argumentTypes := make([]reflect.Type, 0, len(command.Args))
		argumentNodes := make([]parse.Node, 0, len(command.Args))
		for _, argument := range command.Args[1:] {
			argumentType, err := inferNodeType(argument, dotType, variables)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", identifier.Ident, err)
			}
			argumentTypes = append(argumentTypes, argumentType)
			argumentNodes = append(argumentNodes, argument)
		}
		if hasPipedValue {
			argumentTypes = append(argumentTypes, pipedType)
			argumentNodes = append(argumentNodes, nil)
		}

		requireArguments := func(minimum int, maximum int) error {
			if len(argumentTypes) < minimum || (maximum >= 0 && len(argumentTypes) > maximum) {
				return fmt.Errorf("%s: invalid argument count %d", identifier.Ident, len(argumentTypes))
			}
			return nil
		}

		switch identifier.Ident {
		case "index":
			if err := requireArguments(2, -1); err != nil {
				return nil, err
			}
			indexedType := argumentTypes[0]
			for indexPosition, indexType := range argumentTypes[1:] {
				indexedType = indirect(indexedType)
				if indexedType == nil {
					return nil, errors.New("index: cannot index a value of unknown type")
				}
				switch indexedType.Kind() {
				case reflect.Array, reflect.Slice:
					if !isInteger(indexType) {
						return nil, fmt.Errorf("index: index type %v is not an integer", indexType)
					}
					if index, ok := integerLiteral(argumentNodes[indexPosition+1]); ok &&
						(index < 0 || indexedType.Kind() == reflect.Array && index >= int64(indexedType.Len())) {
						return nil, fmt.Errorf("index: literal index %d is out of range", index)
					}
					indexedType = indexedType.Elem()
				case reflect.String:
					if !isInteger(indexType) {
						return nil, fmt.Errorf("index: index type %v is not an integer", indexType)
					}
					if index, ok := integerLiteral(argumentNodes[indexPosition+1]); ok && index < 0 {
						return nil, fmt.Errorf("index: literal index %d is out of range", index)
					}
					indexedType = reflect.TypeFor[uint8]()
				case reflect.Map:
					if !isAssignable(indexType, indexedType.Key()) {
						return nil, fmt.Errorf("index: key type %v is incompatible with %v", indexType, indexedType.Key())
					}
					indexedType = indexedType.Elem()
				default:
					return nil, fmt.Errorf("index: cannot index value of type %v", indexedType)
				}
			}
			return indexedType, nil
		case "len":
			if err := requireArguments(1, 1); err != nil {
				return nil, err
			}
			valueType := indirect(argumentTypes[0])
			if valueType == nil {
				return nil, errors.New("len: cannot inspect a value of unknown type")
			}
			switch valueType.Kind() {
			case reflect.Array, reflect.Chan, reflect.Map, reflect.Slice, reflect.String:
				return reflect.TypeFor[int](), nil
			default:
				return nil, fmt.Errorf("len: cannot inspect value of type %v", valueType)
			}
		case "slice":
			if err := requireArguments(1, 4); err != nil {
				return nil, err
			}
			valueType := indirect(argumentTypes[0])
			if valueType == nil {
				return nil, errors.New("slice: cannot slice a value of unknown type")
			}
			literalIndexes := make([]*int64, len(argumentTypes)-1)
			for indexPosition, indexType := range argumentTypes[1:] {
				if !isInteger(indexType) {
					return nil, fmt.Errorf("slice: index type %v is not an integer", indexType)
				}
				if index, ok := integerLiteral(argumentNodes[indexPosition+1]); ok {
					if index < 0 {
						return nil, fmt.Errorf("slice: literal index %d is out of range", index)
					}
					literalIndexes[indexPosition] = &index
				}
			}
			for indexPosition := 1; indexPosition < len(literalIndexes); indexPosition++ {
				previous := literalIndexes[indexPosition-1]
				current := literalIndexes[indexPosition]
				if previous != nil && current != nil && *previous > *current {
					return nil, fmt.Errorf("slice: invalid literal bounds %d > %d", *previous, *current)
				}
			}
			switch valueType.Kind() {
			case reflect.Array:
				for _, index := range literalIndexes {
					if index != nil && *index > int64(valueType.Len()) {
						return nil, fmt.Errorf("slice: literal index %d is out of range", *index)
					}
				}
				return reflect.SliceOf(valueType.Elem()), nil
			case reflect.Slice:
				return valueType, nil
			case reflect.String:
				if len(argumentTypes) == 4 {
					return nil, errors.New("slice: cannot use three indexes with a string")
				}
				return valueType, nil
			default:
				return nil, fmt.Errorf("slice: cannot slice value of type %v", valueType)
			}
		case "and", "or":
			if err := requireArguments(1, -1); err != nil {
				return nil, err
			}
			resultType := argumentTypes[0]
			for _, argumentType := range argumentTypes[1:] {
				if argumentType != resultType {
					resultType = nil
				}
			}
			return resultType, nil
		case "not":
			if err := requireArguments(1, 1); err != nil {
				return nil, err
			}
			return reflect.TypeFor[bool](), nil
		case "eq", "ne", "lt", "le", "gt", "ge":
			minimum := 2
			maximum := 2
			if identifier.Ident == "eq" {
				maximum = -1
			}
			if err := requireArguments(minimum, maximum); err != nil {
				return nil, err
			}
			comparisonKind := func(dataType reflect.Type) reflect.Kind {
				if dataType == nil {
					return reflect.Invalid
				}
				return dataType.Kind()
			}
			isSignedInteger := func(kind reflect.Kind) bool {
				return kind >= reflect.Int && kind <= reflect.Int64
			}
			isUnsignedInteger := func(kind reflect.Kind) bool {
				return kind >= reflect.Uint && kind <= reflect.Uintptr
			}
			leftType := argumentTypes[0]
			leftKind := comparisonKind(leftType)
			for _, argumentType := range argumentTypes[1:] {
				rightType := argumentType
				rightKind := comparisonKind(rightType)
				leftIsNil := leftType == templateNilType
				rightIsNil := rightType == templateNilType
				if leftIsNil || rightIsNil {
					if identifier.Ident != "eq" && identifier.Ident != "ne" {
						return nil, fmt.Errorf("%s: nil is not ordered", identifier.Ident)
					}
					if leftIsNil && rightIsNil {
						continue
					}
					nonNilType := leftType
					if leftIsNil {
						nonNilType = rightType
					}
					if nonNilType == nil {
						return nil, fmt.Errorf("%s: cannot compare nil with an unknown type", identifier.Ident)
					}
					switch nonNilType.Kind() {
					case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
						continue
					default:
						return nil, fmt.Errorf("%s: type %v cannot be nil", identifier.Ident, nonNilType)
					}
				}
				integerPair := (isSignedInteger(leftKind) || isUnsignedInteger(leftKind)) &&
					(isSignedInteger(rightKind) || isUnsignedInteger(rightKind))
				floatPair := (leftKind == reflect.Float32 || leftKind == reflect.Float64) &&
					(rightKind == reflect.Float32 || rightKind == reflect.Float64)
				complexPair := (leftKind == reflect.Complex64 || leftKind == reflect.Complex128) &&
					(rightKind == reflect.Complex64 || rightKind == reflect.Complex128)
				stringPair := leftKind == reflect.String && rightKind == reflect.String
				boolPair := leftKind == reflect.Bool && rightKind == reflect.Bool
				if identifier.Ident == "eq" || identifier.Ident == "ne" {
					comparablePair := leftKind == rightKind && leftType != nil && rightType != nil &&
						leftKind != reflect.Interface && leftType.Comparable() && rightType.Comparable()
					if !integerPair && !floatPair && !complexPair && !stringPair && !boolPair && !comparablePair {
						return nil, fmt.Errorf("%s: incompatible or non-comparable argument types %v and %v", identifier.Ident, leftType, rightType)
					}
					continue
				}
				orderedPair := integerPair || floatPair || stringPair
				if !orderedPair {
					return nil, fmt.Errorf("%s: unordered or incompatible argument types %v and %v", identifier.Ident, leftType, rightType)
				}
			}
			return reflect.TypeFor[bool](), nil
		case "print", "println", "html", "js", "urlquery":
			return reflect.TypeFor[string](), nil
		case "printf":
			if err := requireArguments(1, -1); err != nil {
				return nil, err
			}
			if argumentTypes[0] == nil || argumentTypes[0].Kind() != reflect.String {
				return nil, fmt.Errorf("printf: format type %v is not a string", argumentTypes[0])
			}
			return reflect.TypeFor[string](), nil
		case "call":
			return nil, errors.New("call: cannot statically validate function calls")
		default:
			return nil, fmt.Errorf("cannot statically validate template command %q", identifier.Ident)
		}
	}

	inferPipeType = func(pipe *parse.PipeNode, dotType reflect.Type, variables templateVariables) (reflect.Type, error) {
		if pipe == nil || len(pipe.Cmds) == 0 {
			return nil, errors.New("cannot validate empty template pipeline")
		}
		var pipedType reflect.Type
		for commandIndex, command := range pipe.Cmds {
			var err error
			pipedType, err = inferCommandType(command, pipedType, commandIndex > 0, dotType, variables)
			if err != nil {
				return nil, err
			}
		}
		return pipedType, nil
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
			pipeType, err := inferPipeType(node, dotType, variables)
			if err != nil {
				return err
			}
			if len(node.Decl) == 1 {
				variables[node.Decl[0].Ident[0]] = pipeType
			}
		case *parse.CommandNode:
			_, err := inferCommandType(node, nil, false, dotType, variables)
			return err
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
			var rangeKeyType reflect.Type
			switch rangeType.Kind() {
			case reflect.Array, reflect.Slice, reflect.Chan:
				rangeKeyType = reflect.TypeFor[int]()
				rangeType = rangeType.Elem()
			case reflect.Map:
				rangeKeyType = rangeType.Key()
				rangeType = rangeType.Elem()
			default:
				return fmt.Errorf("cannot range over type %v", rangeType)
			}
			if len(node.Pipe.Decl) == 1 {
				branchVariables[node.Pipe.Decl[0].Ident[0]] = rangeType
			} else if len(node.Pipe.Decl) == 2 {
				branchVariables[node.Pipe.Decl[0].Ident[0]] = rangeKeyType
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
			if node.Pipe == nil {
				return nil
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

func getItemLocation(request pr_db.PatronRequest, value func(pr_db.PrItem) *string) string {
	for _, item := range request.Items {
		if location := value(item); location != nil && *location != "" {
			return *location
		}
	}
	return DEFAULT_FOR_NO_VALUE
}

func textValue(value pgtype.Text, fallback string) string {
	if value.Valid && value.String != "" {
		return value.String
	}
	return fallback
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

func getPickupURL(request pr_db.PatronRequest) string {
	if deliveryInfo := request.IllResponse.DeliveryInfo; deliveryInfo != nil && deliveryInfo.SentVia != nil && deliveryInfo.SentVia.Text == string(iso18626.SentViaUrl) {
		value := strings.TrimSpace(deliveryInfo.ItemId)
		parsed, err := url.ParseRequestURI(value)
		if err == nil && parsed.Host != "" && (strings.EqualFold(parsed.Scheme, "http") || strings.EqualFold(parsed.Scheme, "https")) {
			return value
		}
	}
	for _, deliveryInfo := range request.IllRequest.RequestedDeliveryInfo {
		if deliveryInfo.Address == nil || deliveryInfo.Address.ElectronicAddress == nil {
			continue
		}
		value := strings.TrimSpace(deliveryInfo.Address.ElectronicAddress.ElectronicAddressData)
		parsed, err := url.ParseRequestURI(value)
		if err == nil && parsed.Host != "" && (strings.EqualFold(parsed.Scheme, "http") || strings.EqualFold(parsed.Scheme, "https")) {
			return value
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
