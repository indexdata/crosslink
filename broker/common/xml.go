package common

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"strings"

	"golang.org/x/text/encoding/ianaindex"
	"golang.org/x/text/encoding/unicode"
)

// UnmarshalXML decodes XML using the encoding precedence from RFC 7303:
// BOM, transport charset, XML declaration, then the XML default of UTF-8.
// transportCharset is an encoding label (for example an HTTP charset parameter),
// or empty when the transport supplies none. UTF-8 and UTF-16 BOMs are supported;
// other charset labels use the encodings supported by x/text's IANA registry.
// Incorrectly labelled UTF-8 is rejected rather than guessed to be a legacy charset.
func UnmarshalXML(data []byte, transportCharset string, value any) error {
	charset := strings.TrimSpace(transportCharset)
	switch {
	case bytes.HasPrefix(data, []byte{0xff, 0xfe, 0x00, 0x00}),
		bytes.HasPrefix(data, []byte{0x00, 0x00, 0xfe, 0xff}):
		// Check UTF-32 before UTF-16, whose little-endian BOM is a prefix of it.
		return fmt.Errorf("unsupported XML BOM: UTF-32")
	case bytes.HasPrefix(data, []byte{0xef, 0xbb, 0xbf}):
		charset = "UTF-8"
		data = data[3:]
	case bytes.HasPrefix(data, []byte{0xfe, 0xff}):
		charset = "UTF-16BE"
		data = data[2:]
	case bytes.HasPrefix(data, []byte{0xff, 0xfe}):
		charset = "UTF-16LE"
		data = data[2:]
	}

	var input io.Reader = bytes.NewReader(data)
	if charset != "" {
		var err error
		input, err = xmlCharsetReader(charset, input)
		if err != nil {
			return err
		}
	}
	decoder := xml.NewDecoder(input)
	decoder.CharsetReader = xmlCharsetReader
	if charset != "" {
		// A BOM or transport charset already determined the encoding. The XML
		// declaration must neither override it nor cause a second conversion.
		decoder.CharsetReader = func(_ string, input io.Reader) (io.Reader, error) {
			return input, nil
		}
	}
	return decoder.Decode(value)
}

func xmlCharsetReader(charset string, input io.Reader) (io.Reader, error) {
	enc, err := ianaindex.MIME.Encoding(charset)
	if err != nil {
		return nil, fmt.Errorf("unsupported XML charset %q: %w", charset, err)
	}
	if enc == nil {
		return nil, fmt.Errorf("unsupported XML charset %q", charset)
	}
	if enc == unicode.UTF8 {
		// Let encoding/xml reject invalid UTF-8. x/text's UTF-8 decoder would
		// replace malformed bytes with U+FFFD and conceal the encoding error.
		return input, nil
	}
	return enc.NewDecoder().Reader(input), nil
}
