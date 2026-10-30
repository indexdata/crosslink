package lms

import (
	"encoding/xml"
	"testing"

	dirapi "github.com/indexdata/crosslink/directory/api"
	"github.com/indexdata/crosslink/ncip"
	ncip1 "github.com/indexdata/crosslink/ncipv1"
	"github.com/stretchr/testify/require"
)

func TestRequestItemScopeEncoding(t *testing.T) {
	normalization, code := "sierra", "LOCAL"
	for _, scope := range []string{"Item", "Title", "Bibliographic Item", "unsupported"} {
		for _, version := range []string{"1.0", "2.02"} {
			t.Run(version+"/"+scope, func(t *testing.T) {
				check1 := func(m ncip1.NCIPMessage) {
					r := m.RequestItem
					require.NotNil(t, r)
					if scope == "Item" {
						require.Nil(t, r.UniqueBibliographicId)
						require.NotNil(t, r.UniqueItemId)
						require.Equal(t, ".b1234", r.UniqueItemId.ItemIdentifierValue)
						require.Equal(t, "lms", r.UniqueItemId.UniqueAgencyId.Value)
					} else {
						require.Nil(t, r.UniqueItemId)
						require.Equal(t, "123", r.UniqueBibliographicId.BibliographicRecordId.BibliographicRecordIdentifier)
						require.Equal(t, code, r.UniqueBibliographicId.BibliographicRecordId.BibliographicRecordIdentifierCode.Value)
					}
				}
				if version == "1.0" {
					called := false
					a := ncip1TestAdapter(t, `<RequestItemResponse>`+ncip1TestHeader+`<UniqueRequestId><UniqueAgencyId><Scheme>urn:test:agency</Scheme><Value>lms</Value></UniqueAgencyId><RequestIdentifierValue>request</RequestIdentifierValue></UniqueRequestId>`+ncip1TestUser+`<RequestType><Scheme>urn:test:type</Scheme><Value>Loan</Value></RequestType><RequestScopeType><Scheme>urn:test:scope</Scheme><Value>Item</Value></RequestScopeType></RequestItemResponse>`, func(m ncip1.NCIPMessage) { called = true; check1(m) })
					a.config.RequestItemRequestScopeType = &scope
					a.config.BibIdNormalization = &normalization
					a.config.RequestItemBibIdCode = &code
					_, err := a.RequestItem("request", RequestItemIdentifier{BibliographicID: ".b1234"}, "user", "", "")
					if scope == "unsupported" {
						require.ErrorContains(t, err, "unsupported")
						require.False(t, called)
					} else {
						require.NoError(t, err)
						require.True(t, called)
					}
				} else {
					mock := new(ncipClientMock)
					a := &LmsAdapterNcip{config: dirapi.LmsConfig{RequestItemRequestScopeType: &scope, BibIdNormalization: &normalization, RequestItemBibIdCode: &code}, ncipClient: mock}
					_, err := a.RequestItem("request", RequestItemIdentifier{BibliographicID: ".b1234"}, "user", "", "")
					if scope == "unsupported" {
						require.ErrorContains(t, err, "unsupported")
						require.Nil(t, mock.lastRequest)
						return
					}
					require.NoError(t, err)
					data, err := xml.Marshal(mock.lastRequest)
					require.NoError(t, err)
					var r ncip.RequestItem
					require.NoError(t, xml.Unmarshal(data, &r))
					if scope == "Item" {
						require.Empty(t, r.BibliographicId)
						require.Len(t, r.ItemId, 1)
						require.Equal(t, ".b1234", r.ItemId[0].ItemIdentifierValue)
					} else {
						require.Empty(t, r.ItemId)
						require.Equal(t, "123", r.BibliographicId[0].BibliographicRecordId.BibliographicRecordIdentifier)
						require.Equal(t, code, r.BibliographicId[0].BibliographicRecordId.BibliographicRecordIdentifierCode.Text)
					}
				}
			})
		}
	}
}

func TestRequestItemRejectsEmptyIdentifier(t *testing.T) {
	for _, scope := range []string{"Item", "Title", "Bibliographic Item"} {
		for _, id := range []string{"", "   ", ".b"} {
			if scope == "Item" && id == ".b" {
				continue
			}
			t.Run(scope+"/"+id, func(t *testing.T) {
				normalization := "sierra"
				cfg := dirapi.LmsConfig{RequestItemRequestScopeType: &scope, BibIdNormalization: &normalization}
				mock := new(ncipClientMock)
				a := &LmsAdapterNcip{config: cfg, ncipClient: mock}
				_, err := a.RequestItem("request", RequestItemIdentifier{BibliographicID: id}, "user", "", "")
				require.Error(t, err)
				require.Nil(t, mock.lastRequest)
				a1 := &LmsAdapterNcip1{config: cfg}
				_, err = a1.RequestItem("request", RequestItemIdentifier{BibliographicID: id}, "user", "", "")
				require.Error(t, err) // nil client would panic if validation sent a message
			})
		}
	}
}

func TestValidateSelectedItemScope(t *testing.T) {
	for _, scope := range []string{"auto", "Item", "Title", "Bibliographic Item", "unsupported"} {
		for _, version := range []string{"1.0", "2.02"} {
			t.Run(version+"/"+scope, func(t *testing.T) {
				entry := dirapi.Entry{LmsConfig: &dirapi.LmsConfig{Address: "https://lms.example/ncip", FromAgency: "broker", RequestItemRequestScopeType: &scope, NcipVersion: &version}}
				err := ValidateSelectedItemScope(entry)
				if scope == "Item" || scope == "auto" {
					require.NoError(t, err)
				} else {
					require.Error(t, err)
				}
				disabled := false
				entry.LmsConfig.RequestItemEnabled = &disabled
				require.NoError(t, ValidateSelectedItemScope(entry))
			})
		}
	}
	require.NoError(t, ValidateSelectedItemScope(dirapi.Entry{}))
}

func TestValidateSelectedItemScopeForAdapter(t *testing.T) {
	for _, scope := range []string{"auto", "Item", "Title", "Bibliographic Item", "unsupported"} {
		for _, enabled := range []bool{false, true} {
			cfg := dirapi.LmsConfig{RequestItemRequestScopeType: &scope, RequestItemEnabled: &enabled}
			for _, adapter := range []LmsAdapter{&LmsAdapterNcip{config: cfg}, &LmsAdapterNcip1{config: cfg}} {
				err := ValidateSelectedItemScopeForAdapter(adapter)
				if !enabled || scope == "Item" || scope == "auto" {
					require.NoError(t, err)
				} else {
					require.Error(t, err)
				}
			}
		}
	}
	require.NoError(t, ValidateSelectedItemScopeForAdapter(&LmsAdapterManual{}))
}

func TestAutomaticRequestScope(t *testing.T) {
	for _, configured := range []*string{nil, new("auto")} {
		for _, item := range []string{"", ".b5678"} {
			for _, version := range []string{"1.0", "2.02"} {
				t.Run(version+"/"+item, func(t *testing.T) {
					id := RequestItemIdentifier{BibliographicID: ".b1234", ItemID: item}
					scope := "Bibliographic Item"
					if item != "" {
						scope = "Item"
					}
					if version == "1.0" {
						a := ncip1TestAdapter(t, `<RequestItemResponse>`+ncip1TestHeader+`<UniqueRequestId><UniqueAgencyId><Scheme>urn:test:agency</Scheme><Value>lms</Value></UniqueAgencyId><RequestIdentifierValue>request</RequestIdentifierValue></UniqueRequestId>`+ncip1TestUser+`<RequestType><Scheme>urn:test:type</Scheme><Value>Loan</Value></RequestType><RequestScopeType><Scheme>urn:test:scope</Scheme><Value>`+scope+`</Value></RequestScopeType></RequestItemResponse>`, func(m ncip1.NCIPMessage) {
							require.Equal(t, scope, m.RequestItem.RequestScopeType.Value)
							if item != "" {
								require.Nil(t, m.RequestItem.UniqueBibliographicId)
								require.Equal(t, item, m.RequestItem.UniqueItemId.ItemIdentifierValue)
							} else {
								require.Nil(t, m.RequestItem.UniqueItemId)
								require.Equal(t, "123", m.RequestItem.UniqueBibliographicId.BibliographicRecordId.BibliographicRecordIdentifier)
							}
						})
						a.config.RequestItemRequestScopeType = configured
						a.config.BibIdNormalization = new("sierra")
						_, err := a.RequestItem("request", id, "user", "", "")
						require.NoError(t, err)
					} else {
						mock := new(ncipClientMock)
						a := &LmsAdapterNcip{config: dirapi.LmsConfig{RequestItemRequestScopeType: configured, BibIdNormalization: new("sierra")}, ncipClient: mock}
						_, err := a.RequestItem("request", id, "user", "", "")
						require.NoError(t, err)
						r := mock.lastRequest.(ncip.RequestItem)
						require.Equal(t, scope, r.RequestScopeType.Text)
						if item != "" {
							require.Empty(t, r.BibliographicId)
							require.Equal(t, item, r.ItemId[0].ItemIdentifierValue)
						} else {
							require.Empty(t, r.ItemId)
							require.Equal(t, "123", r.BibliographicId[0].BibliographicRecordId.BibliographicRecordIdentifier)
						}
					}
				})
			}
		}
	}
}
