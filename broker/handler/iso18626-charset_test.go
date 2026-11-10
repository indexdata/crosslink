package handler

import (
	"bytes"
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/indexdata/crosslink/broker/adapter"
	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/events"
	"github.com/indexdata/crosslink/broker/ill_db"
	"github.com/indexdata/crosslink/broker/test/mocks"
	"github.com/indexdata/crosslink/iso18626"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/charmap"
)

type charsetRequestRepo struct {
	mocks.MockIllRepositorySuccess
	saved *ill_db.SaveIllTransactionParams
}

func (r *charsetRequestRepo) SaveIllTransaction(_ common.ExtendedContext, params ill_db.SaveIllTransactionParams) (ill_db.IllTransaction, error) {
	r.saved = &params
	return ill_db.IllTransaction(params), nil
}

func TestIso18626PostHandlerCharset(t *testing.T) {
	tests := []struct {
		name          string
		contentType   string
		encoding      encoding.Encoding
		bom           bool
		noDeclaration bool
		declaration   string
		title         string
		wantTitle     string
		status        int
	}{
		{name: "UTF-8 default", contentType: "application/xml", title: "Anaïs Nin © — 🙂", status: http.StatusOK},
		{name: "Latin-1 transport overrides UTF-8 declaration", contentType: "application/xml; charset=ISO-8859-1", encoding: charmap.ISO8859_1, title: "Götterzorn / Anaïs Nin ©", status: http.StatusOK},
		{name: "Latin-1 spaced declaration", contentType: "application/xml", encoding: charmap.ISO8859_1, declaration: `<?xml version='1.0' encoding = 'ISO-8859-1'?>`, title: "Götterzorn / Anaïs Nin ©", status: http.StatusOK},
		{name: "unsupported spaced declaration", contentType: "application/xml", declaration: `<?xml version='1.0' encoding = 'unknown'?>`, title: "Nin", status: http.StatusBadRequest},
		{name: "Windows-1252 quoted charset", contentType: `text/xml; charset="windows-1252"`, encoding: charmap.Windows1252, title: "Anaïs Nin © —", status: http.StatusOK},
		{name: "case insensitive media type and charset parameter", contentType: `Application/XML; Charset="ISO-8859-1"`, encoding: charmap.ISO8859_1, title: "Götterzorn", status: http.StatusOK},
		{name: "BOM overrides transport charset", contentType: "application/xml; charset=ISO-8859-1", bom: true, title: "Anaïs Nin 🙂", status: http.StatusOK},
		{name: "BOM overrides unsupported transport charset", contentType: "application/xml; charset=unknown", bom: true, title: "Anaïs Nin 🙂", status: http.StatusOK},
		{name: "invalid UTF-8 declaration is replaced", contentType: "application/xml", encoding: charmap.ISO8859_1, title: "Anaïs Nin", wantTitle: "Ana�s Nin", status: http.StatusOK},
		{name: "invalid UTF-8 transport is replaced", contentType: "application/xml; charset=UTF-8", encoding: charmap.ISO8859_1, title: "Anaïs Nin", wantTitle: "Ana�s Nin", status: http.StatusOK},
		{name: "invalid UTF-8 BOM is replaced", contentType: "application/xml; charset=ISO-8859-1", encoding: charmap.ISO8859_1, bom: true, title: "Anaïs Nin", wantTitle: "Ana�s Nin", status: http.StatusOK},
		{name: "invalid UTF-8 default is replaced", contentType: "application/xml", encoding: charmap.ISO8859_1, noDeclaration: true, title: "Anaïs Nin", wantTitle: "Ana�s Nin", status: http.StatusOK},
		{name: "unsupported charset", contentType: "application/xml; charset=unknown", title: "Nin", status: http.StatusBadRequest},
		{name: "malformed charset parameter", contentType: "application/xml; charset", title: "Nin", status: http.StatusUnsupportedMediaType},
		{name: "invalid media type prefix", contentType: "application/xml-invalid", title: "Nin", status: http.StatusUnsupportedMediaType},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			message := iso18626.NewISO18626Message()
			message.Request = &iso18626.Request{
				Header: iso18626.Header{
					RequestingAgencyRequestId: "charset-test",
					RequestingAgencyId: iso18626.TypeAgencyId{
						AgencyIdType:  iso18626.TypeSchemeValuePair{Text: "ISIL"},
						AgencyIdValue: "REQ",
					},
					SupplyingAgencyId: iso18626.TypeAgencyId{
						AgencyIdType:  iso18626.TypeSchemeValuePair{Text: "ISIL"},
						AgencyIdValue: "BROKER",
					},
				},
				BibliographicInfo: iso18626.BibliographicInfo{Title: tt.title},
			}
			body, err := xml.Marshal(message)
			require.NoError(t, err)
			if !tt.noDeclaration {
				declaration := tt.declaration
				if declaration == "" {
					declaration = xml.Header
				}
				body = append([]byte(declaration), body...)
			}
			if tt.encoding != nil {
				body, err = tt.encoding.NewEncoder().Bytes(body)
				require.NoError(t, err)
			}
			if tt.bom {
				body = append([]byte{0xef, 0xbb, 0xbf}, body...)
			}
			repo := &charsetRequestRepo{}
			eventBus := events.NewPostgresEventBus(&mocks.MockEventRepositorySuccess{}, "mock")
			request := httptest.NewRequest(http.MethodPost, "/iso18626", bytes.NewReader(body))
			request.Header.Set("Content-Type", tt.contentType)
			response := httptest.NewRecorder()
			Iso18626PostHandler(repo, eventBus, &adapter.MockDirectoryLookupAdapter{}, 10000)(response, request)

			require.Equal(t, tt.status, response.Code, response.Body.String())
			if tt.status != http.StatusOK {
				assert.Nil(t, repo.saved, "rejected requests must not be persisted")
				return
			}
			require.NotNil(t, repo.saved)
			wantTitle := tt.wantTitle
			if wantTitle == "" {
				wantTitle = tt.title
			}
			assert.Equal(t, wantTitle, repo.saved.IllTransactionData.BibliographicInfo.Title)
			var confirmation iso18626.ISO18626Message
			require.NoError(t, xml.Unmarshal(response.Body.Bytes(), &confirmation))
			require.NotNil(t, confirmation.RequestConfirmation)
			assert.Equal(t, iso18626.TypeMessageStatusOK, confirmation.RequestConfirmation.ConfirmationHeader.MessageStatus)
		})
	}
}
