package client

import (
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/indexdata/crosslink/broker/events"
	"github.com/indexdata/crosslink/broker/ill_db"
	"github.com/indexdata/crosslink/iso18626"
	"github.com/indexdata/go-utils/utils"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
)

func TestCreateMessageHeaderTransparent(t *testing.T) {
	var client = CreateIso18626Client(new(events.PostgresEventBus), new(ill_db.PgIllRepo), 1, BrokerModeTransparent, 0*time.Second)
	illTrans := ill_db.IllTransaction{RequesterSymbol: pgtype.Text{String: "ISIL:REQ"}}
	sup := ill_db.LocatedSupplier{SupplierSymbol: "ISIL:SUP"}

	reqHeader := client.createMessageHeader(illTrans, &sup, true)
	assert.Equal(t, "REQ", reqHeader.RequestingAgencyId.AgencyIdValue)
	assert.Equal(t, "SUP", reqHeader.SupplyingAgencyId.AgencyIdValue)

	supHeader := client.createMessageHeader(illTrans, &sup, false)
	assert.Equal(t, "REQ", supHeader.RequestingAgencyId.AgencyIdValue)
	assert.Equal(t, "SUP", supHeader.SupplyingAgencyId.AgencyIdValue)
}

func TestCreateMessageHeaderOpaque(t *testing.T) {
	var client = CreateIso18626Client(new(events.PostgresEventBus), new(ill_db.PgIllRepo), 1, BrokerModeOpaque, 0*time.Second)
	illTrans := ill_db.IllTransaction{RequesterSymbol: pgtype.Text{String: "ISIL:REQ"}}
	sup := ill_db.LocatedSupplier{SupplierSymbol: "ISIL:SUP"}

	reqHeader := client.createMessageHeader(illTrans, &sup, true)
	assert.Equal(t, "BROKER", reqHeader.RequestingAgencyId.AgencyIdValue)
	assert.Equal(t, "SUP", reqHeader.SupplyingAgencyId.AgencyIdValue)

	supHeader := client.createMessageHeader(illTrans, &sup, false)
	assert.Equal(t, "REQ", supHeader.RequestingAgencyId.AgencyIdValue)
	assert.Equal(t, "BROKER", supHeader.SupplyingAgencyId.AgencyIdValue)
}

func TestSendHttpPost(t *testing.T) {
	headers := map[string]string{
		"X-Okapi-Tenant": "mytenant",
		"X-Other":        "myother",
	}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		assert.Equal(t, "mytenant", r.Header.Get("X-Okapi-Tenant"))
		assert.Equal(t, "myother", r.Header.Get("X-Other"))
		w.WriteHeader(http.StatusOK)
		msg := &iso18626.ISO18626Message{}
		buf := utils.Must(xml.Marshal(msg))
		_, err := w.Write(buf)
		assert.Nil(t, err)
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	var client = CreateIso18626Client(new(events.PostgresEventBus), new(ill_db.PgIllRepo), 1000, BrokerModeOpaque, 0*time.Second)

	msg := &iso18626.ISO18626Message{}
	peer := ill_db.Peer{
		Url:         server.URL,
		HttpHeaders: headers,
	}
	_, err := client.SendHttpPost(&peer, msg)
	assert.Nil(t, err)
}
