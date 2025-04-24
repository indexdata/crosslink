package client

import (
	"github.com/indexdata/crosslink/broker/events"
	"github.com/indexdata/crosslink/broker/ill_db"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestCreateMessageHeaderTransparent(t *testing.T) {
	var client = CreateIso18626Client(new(events.PostgresEventBus), new(ill_db.PgIllRepo), 1, BrokerModeTransparent)
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
	var client = CreateIso18626Client(new(events.PostgresEventBus), new(ill_db.PgIllRepo), 1, BrokerModeOpaque)
	illTrans := ill_db.IllTransaction{RequesterSymbol: pgtype.Text{String: "ISIL:REQ"}}
	sup := ill_db.LocatedSupplier{SupplierSymbol: "ISIL:SUP"}

	reqHeader := client.createMessageHeader(illTrans, &sup, true)
	assert.Equal(t, "BROKER", reqHeader.RequestingAgencyId.AgencyIdValue)
	assert.Equal(t, "SUP", reqHeader.SupplyingAgencyId.AgencyIdValue)

	supHeader := client.createMessageHeader(illTrans, &sup, false)
	assert.Equal(t, "REQ", supHeader.RequestingAgencyId.AgencyIdValue)
	assert.Equal(t, "BROKER", supHeader.SupplyingAgencyId.AgencyIdValue)
}
