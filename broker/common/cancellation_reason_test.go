package common

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestExtractCancellationReason(t *testing.T) {
	for _, tt := range []struct{ name, note, reason, remainder string }{
		{"code", "#CancellationReason: InvalidUser#", "InvalidUser", ""},
		{"text", "#CancellationReason: Duplicate Request#", "Duplicate Request", ""},
		{"immediate label suffix", "#CancellationReason: InvalidUser#Ref: 123#", "InvalidUser", "Ref: 123#"},
		{"immediate word suffix", "#CancellationReason: InvalidUser#followup#", "InvalidUser", "followup#"},
		{"suffix before condition marker", "#CancellationReason: InvalidUser#followup#ReShareLoanConditionRejectResponse#", "InvalidUser", "followup#ReShareLoanConditionRejectResponse#"},
		{"surrounding", "before\n#CancellationReason:  InvalidUser \t# after", "InvalidUser", "before\n after"},
		{"adjacent marker", "#CancellationReason: InvalidUser##ReShareLoanConditionRejectResponse#", "InvalidUser", "#ReShareLoanConditionRejectResponse#"},
		{"preceding marker", "#ReShareLoanConditionRejectResponse##CancellationReason: InvalidUser#", "InvalidUser", "#ReShareLoanConditionRejectResponse#"},
		{"multiple", "#CancellationReason: First# / #CancellationReason: Second#", "First", " / "},
		{"empty then valid", "#CancellationReason: # #CancellationReason: Valid#", "Valid", "#CancellationReason: # "},
		{"empty", "#CancellationReason: \t#", "", "#CancellationReason: \t#"},
		{"empty shared delimiter", "#CancellationReason:#CancellationReason: Valid#", "", "#CancellationReason:#CancellationReason: Valid#"},
		{"unterminated", "before #CancellationReason: InvalidUser", "", "before #CancellationReason: InvalidUser"},
		{"first hash closes reason before condition text", "#CancellationReason: broken #ReShareLoanConditionRejectResponse#", "broken", "ReShareLoanConditionRejectResponse#"},
		{"first hash closes reason before other text", "#CancellationReason: broken #OtherMarker#", "broken", "OtherMarker#"},
		{"shared hash closes first reason", "#CancellationReason: broken #CancellationReason: Valid#", "broken", "CancellationReason: Valid#"},
		{"wrong case", "#cancellationReason: InvalidUser#", "", "#cancellationReason: InvalidUser#"},
		{"no marker", "ordinary note #OtherMarker#", "", "ordinary note #OtherMarker#"},
		{"empty note", "", "", ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			reason, note := ExtractCancellationReason(tt.note)
			assert.Equal(t, tt.reason, reason)
			assert.Equal(t, tt.remainder, note)
		})
	}
}
