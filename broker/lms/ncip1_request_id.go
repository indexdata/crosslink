package lms

import (
	"encoding/base64"
	"fmt"
	"strings"

	ncip "github.com/indexdata/crosslink/ncipv1"
)

// Returned v1 request IDs are opaque to the broker. Persist all three parts so
// later adapters (including after a restart) can address the owning agency.
// Only cancellation and checkout decode this reserved prefix; initiation IDs
// supplied to AcceptItem and RequestItem remain literal broker-owned IDs.
const ncip1RequestPrefix = "ncip1-request:"

func encodeNcip1RequestID(id ncip.UniqueRequestId) string {
	return encodeNcip1Reference(ncip1RequestPrefix, id.UniqueAgencyId, id.RequestIdentifierValue)
}

const ncip1ItemPrefix = "ncip1-item:"

func encodeNcip1ItemID(id ncip.UniqueItemId) string {
	return encodeNcip1Reference(ncip1ItemPrefix, id.UniqueAgencyId, id.ItemIdentifierValue)
}

func encodeNcip1Reference(prefix string, agency ncip.UniqueAgencyId, value string) string {
	parts := []string{agency.Scheme, agency.Value, value}
	for i, part := range parts {
		parts[i] = base64.RawURLEncoding.EncodeToString([]byte(part))
	}
	return prefix + strings.Join(parts, ".")
}

func (l *LmsAdapterNcip1) savedRequest(id string) (*ncip.UniqueRequestId, error) {
	if !strings.HasPrefix(id, ncip1RequestPrefix) {
		// Legacy IDs did not store an agency; preserve their existing behavior.
		return l.request(id), nil
	}
	agency, value, err := decodeNcip1Reference(id, ncip1RequestPrefix, "request")
	if err != nil {
		return nil, err
	}
	return &ncip.UniqueRequestId{UniqueAgencyId: agency, RequestIdentifierValue: value}, nil
}

// savedItem restores returned LMS identities; plain barcodes retain the configured agency.
func (l *LmsAdapterNcip1) savedItem(id string) (ncip.UniqueItemId, error) {
	if !strings.HasPrefix(id, ncip1ItemPrefix) {
		return l.item(id), nil
	}
	agency, value, err := decodeNcip1Reference(id, ncip1ItemPrefix, "item")
	if err != nil {
		return ncip.UniqueItemId{}, err
	}
	return ncip.UniqueItemId{UniqueAgencyId: agency, ItemIdentifierValue: value}, nil
}

func decodeNcip1Reference(id, prefix, kind string) (ncip.UniqueAgencyId, string, error) {
	parts := strings.Split(strings.TrimPrefix(id, prefix), ".")
	if len(parts) != 3 {
		return ncip.UniqueAgencyId{}, "", fmt.Errorf("invalid NCIP v1 %s reference", kind)
	}
	for i, part := range parts {
		decoded, err := base64.RawURLEncoding.Strict().DecodeString(part)
		if err != nil || strings.TrimSpace(string(decoded)) == "" {
			return ncip.UniqueAgencyId{}, "", fmt.Errorf("invalid NCIP v1 %s reference", kind)
		}
		parts[i] = string(decoded)
	}
	return ncip.UniqueAgencyId{Scheme: parts[0], Value: parts[1]}, parts[2], nil
}
