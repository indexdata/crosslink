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
	parts := []string{id.UniqueAgencyId.Scheme, id.UniqueAgencyId.Value, id.RequestIdentifierValue}
	for i, part := range parts {
		parts[i] = base64.RawURLEncoding.EncodeToString([]byte(part))
	}
	return ncip1RequestPrefix + strings.Join(parts, ".")
}

func (l *LmsAdapterNcip1) savedRequest(id string) (*ncip.UniqueRequestId, error) {
	if !strings.HasPrefix(id, ncip1RequestPrefix) {
		// Legacy IDs did not store an agency; preserve their existing behavior.
		return l.request(id), nil
	}
	parts := strings.Split(strings.TrimPrefix(id, ncip1RequestPrefix), ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("invalid NCIP v1 request reference")
	}
	for i, part := range parts {
		decoded, err := base64.RawURLEncoding.Strict().DecodeString(part)
		if err != nil || strings.TrimSpace(string(decoded)) == "" {
			return nil, fmt.Errorf("invalid NCIP v1 request reference")
		}
		parts[i] = string(decoded)
	}
	return &ncip.UniqueRequestId{UniqueAgencyId: ncip.UniqueAgencyId{Scheme: parts[0], Value: parts[1]}, RequestIdentifierValue: parts[2]}, nil
}
