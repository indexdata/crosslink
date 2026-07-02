package email

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// ---------------------------------------------------------------------------
// joinAddresses
// ---------------------------------------------------------------------------

func TestJoinAddresses_Empty(t *testing.T) {
	assert.Equal(t, "", joinAddresses(nil))
	assert.Equal(t, "", joinAddresses([]string{}))
}

func TestJoinAddresses_Single(t *testing.T) {
	assert.Equal(t, "a@b.com", joinAddresses([]string{"a@b.com"}))
}

func TestJoinAddresses_Multiple(t *testing.T) {
	assert.Equal(t, "a@b.com, c@d.com, e@f.com",
		joinAddresses([]string{"a@b.com", "c@d.com", "e@f.com"}))
}

// ---------------------------------------------------------------------------
// buildRawMessage
// ---------------------------------------------------------------------------

func TestBuildRawMessage_PlainTextHeaders(t *testing.T) {
	data := EmailData{
		To:      []string{"to@example.com"},
		Subject: "Hello",
		Body:    "Plain text body",
	}
	raw, err := BuildRawMessage("from@example.com", data, nil)
	assert.NoError(t, err)
	msg := string(raw)
	assert.Contains(t, msg, "From: from@example.com")
	assert.Contains(t, msg, "To: to@example.com")
	assert.Contains(t, msg, "Subject:")
	assert.Contains(t, msg, "MIME-Version: 1.0")
	assert.Contains(t, msg, "text/plain")
	assert.NotContains(t, msg, "application/pdf")
}

func TestBuildRawMessage_HTMLBody(t *testing.T) {
	data := EmailData{
		To:     []string{"to@example.com"},
		Body:   "<p>HTML body</p>",
		IsHTML: true,
	}
	raw, err := BuildRawMessage("from@example.com", data, nil)
	assert.NoError(t, err)
	assert.Contains(t, string(raw), "text/html")
}

func TestBuildRawMessage_MultipleRecipients(t *testing.T) {
	data := EmailData{
		To:   []string{"a@b.com", "c@d.com"},
		Body: "body",
	}
	raw, err := BuildRawMessage("from@example.com", data, nil)
	assert.NoError(t, err)
	assert.Contains(t, string(raw), "a@b.com, c@d.com")
}

func TestBuildRawMessage_WithPDFAttachment(t *testing.T) {
	data := EmailData{
		To:   []string{"to@example.com"},
		Body: "body with attachment",
	}
	att := &PdfAttach{Filename: "pull-slips.pdf", Data: []byte("%PDF-1.4 fake")}
	raw, err := BuildRawMessage("from@example.com", data, att)
	assert.NoError(t, err)
	msg := string(raw)
	assert.Contains(t, msg, "application/pdf")
	assert.Contains(t, msg, `attachment; filename="pull-slips.pdf"`)
	assert.Contains(t, msg, "Content-Transfer-Encoding: base64")
}

func TestBuildRawMessage_WithoutAttachment(t *testing.T) {
	data := EmailData{To: []string{"to@example.com"}, Body: "body"}
	raw, err := BuildRawMessage("from@example.com", data, nil)
	assert.NoError(t, err)
	assert.NotContains(t, string(raw), "application/pdf")
}
