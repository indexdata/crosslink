package email

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/smtp"
	"net/textproto"
	"strings"

	"github.com/indexdata/go-utils/utils"
)

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
