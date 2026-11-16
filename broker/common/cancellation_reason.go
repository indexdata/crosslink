package common

import "strings"

const cancellationReasonPrefix = "#CancellationReason:"

// ExtractCancellationReason removes complete, nonblank cancellation reason
// markers and returns the first reason, trimmed. All other note bytes are kept.
// The first hash after the prefix closes the marker; reasons cannot contain hashes.
// Empty markers and prefixes without a closing hash are left intact.
func ExtractCancellationReason(note string) (reason, remainder string) {
	var out strings.Builder
	for {
		start := strings.Index(note, cancellationReasonPrefix)
		if start < 0 {
			out.WriteString(note)
			return reason, out.String()
		}
		valueStart := start + len(cancellationReasonPrefix)
		offset := strings.IndexByte(note[valueStart:], '#')
		if offset < 0 {
			out.WriteString(note)
			return reason, out.String()
		}
		end := valueStart + offset
		value := strings.TrimSpace(note[valueStart:end])
		if value == "" {
			out.WriteString(note[:end+1])
			note = note[end+1:]
			continue
		}
		out.WriteString(note[:start])
		if reason == "" {
			reason = value
		}
		note = note[end+1:]
	}
}
