package common

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"regexp"
	"strings"

	"golang.org/x/text/encoding/ianaindex"
)

// Extract the encoding label from the initial declaration without validating it.
// Stop at '?' so a declaration without encoding cannot match document content.
var xmlDeclEncoding = regexp.MustCompile(`^<\?xml\s+[^?]*\sencoding\s*=\s*["']([^"']+)["']`)

// UnmarshalXML decodes XML using the encoding precedence from RFC 7303:
// BOM, transport charset, XML declaration, then the XML default of UTF-8.
// transportCharset is an encoding label (for example an HTTP charset parameter),
// or empty when the transport supplies none. UTF-8 and UTF-16 BOMs are supported;
// other charset labels use the encodings supported by x/text's IANA registry.
// Malformed character sequences use the selected decoder's U+FFFD replacement
// behavior, including for UTF-8. XML syntax errors are still rejected.
// The returned charset includes its source, for example "UTF-8 (declaration)".
func UnmarshalXML(data []byte, transportCharset string, value any) (string, error) {
	charset := strings.TrimSpace(transportCharset)
	source := "Content-Type"
	switch {
	case bytes.HasPrefix(data, []byte{0xff, 0xfe, 0x00, 0x00}),
		bytes.HasPrefix(data, []byte{0x00, 0x00, 0xfe, 0xff}):
		// Check UTF-32 before UTF-16, whose little-endian BOM is a prefix of it.
		return "", fmt.Errorf("unsupported XML BOM: UTF-32")
	case bytes.HasPrefix(data, []byte{0xef, 0xbb, 0xbf}):
		charset, source = "UTF-8", "BOM"
		data = data[3:]
	case bytes.HasPrefix(data, []byte{0xfe, 0xff}):
		charset, source = "UTF-16BE", "BOM"
		data = data[2:]
	case bytes.HasPrefix(data, []byte{0xff, 0xfe}):
		charset, source = "UTF-16LE", "BOM"
		data = data[2:]
	}

	if charset == "" {
		charset, source = xmlDeclarationCharset(data)
	}
	input, err := xmlCharsetReader(charset, bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	decoder := xml.NewDecoder(input)
	// Every input is now converted to UTF-8. The declaration is still parsed,
	// but must not trigger a second conversion or override the chosen encoding.
	decoder.CharsetReader = func(_ string, input io.Reader) (io.Reader, error) {
		return input, nil
	}
	return fmt.Sprintf("%s (%s)", charset, source), decoder.Decode(value)
}

func xmlDeclarationCharset(data []byte) (string, string) {
	if match := xmlDeclEncoding.FindSubmatch(data); match != nil {
		return string(match[1]), "declaration"
	}
	return "UTF-8", "default"
}

func xmlCharsetReader(charset string, input io.Reader) (io.Reader, error) {
	enc, err := ianaindex.MIME.Encoding(charset)
	if err != nil {
		return nil, fmt.Errorf("unsupported XML charset %q: %w", charset, err)
	}
	if enc == nil {
		return nil, fmt.Errorf("unsupported XML charset %q", charset)
	}
	return enc.NewDecoder().Reader(input), nil
}
