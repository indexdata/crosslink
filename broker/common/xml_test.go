package common

import (
	"encoding/binary"
	"encoding/xml"
	"testing"
	"unicode/utf16"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUnmarshalXML(t *testing.T) {
	document := func(charset, text string) []byte {
		declaration := ""
		if charset != "" {
			declaration = `<?xml version="1.0" encoding="` + charset + `"?>`
		}
		return []byte(declaration + "<document><title>" + text + "</title></document>")
	}
	utf16Document := func(charset, text string, order binary.ByteOrder, bom bool) []byte {
		units := utf16.Encode([]rune(string(document(charset, text))))
		if bom {
			units = append([]uint16{0xfeff}, units...)
		}
		data := make([]byte, 2*len(units))
		for i, unit := range units {
			order.PutUint16(data[2*i:], unit)
		}
		return data
	}
	utf8BOM := func(data []byte) []byte { return append([]byte{0xef, 0xbb, 0xbf}, data...) }
	tests := []struct {
		name    string
		data    []byte
		charset string
		want    string
		err     string
	}{
		{name: "UTF-8 default", data: document("", "Anaïs © — 🙂"), want: "Anaïs © — 🙂"},
		{name: "UTF-8 declaration", data: document("UTF-8", "Götterzorn"), want: "Götterzorn"},
		{name: "Latin-1 declaration", data: document("ISO-8859-1", "Ana\xefs Nin"), want: "Anaïs Nin"},
		{name: "Windows-1252 declaration", data: document("windows-1252", "\x93Nin\x94 \x97 \xa9 \x80"), want: "“Nin” — © €"},
		{name: "Shift-JIS declaration", data: document("Shift_JIS", "\x93\xfa\x96\x7b"), want: "日本"},
		{name: "transport overrides UTF-8 declaration", data: document("UTF-8", "G\xf6tterzorn"), charset: "ISO-8859-1", want: "Götterzorn"},
		{name: "transport without declaration", data: document("", "Ana\xefs Nin"), charset: "ISO-8859-1", want: "Anaïs Nin"},
		{name: "transport overrides legacy declaration", data: document("ISO-8859-1", "\x80\x97"), charset: "windows-1252", want: "€—"},
		{name: "transport conversion happens once", data: document("ISO-8859-1", "Ana\xefs Nin"), charset: "ISO-8859-1", want: "Anaïs Nin"},
		{name: "UTF-8 transport overrides legacy declaration", data: document("ISO-8859-1", "Anaïs © 🙂"), charset: "UTF-8", want: "Anaïs © 🙂"},
		{name: "transport charset alias", data: document("UTF-8", "Ana\xefs"), charset: " LaTiN1 ", want: "Anaïs"},
		{name: "transport overrides unsupported declaration", data: document("unknown", "Anaïs"), charset: "UTF-8", want: "Anaïs"},
		{name: "UTF-8 BOM overrides transport and declaration", data: utf8BOM(document("ISO-8859-1", "Anaïs © 🙂")), charset: "windows-1252", want: "Anaïs © 🙂"},
		{name: "UTF-8 BOM overrides unsupported charset", data: utf8BOM(document("unknown", "Anaïs")), charset: "unknown", want: "Anaïs"},
		{name: "UTF-16LE BOM overrides transport and declaration", data: utf16Document("ISO-8859-1", "Anaïs 🙂", binary.LittleEndian, true), charset: "UTF-8", want: "Anaïs 🙂"},
		{name: "UTF-16BE BOM overrides unsupported charset", data: utf16Document("unknown", "Götterzorn 🙂", binary.BigEndian, true), charset: "unknown", want: "Götterzorn 🙂"},
		{name: "UTF-16LE transport", data: utf16Document("UTF-8", "Anaïs", binary.LittleEndian, false), charset: "UTF-16LE", want: "Anaïs"},
		{name: "UTF-16BE transport", data: utf16Document("UTF-8", "Götterzorn", binary.BigEndian, false), charset: "UTF-16BE", want: "Götterzorn"},
		{name: "UTF-16 transport without BOM defaults to big endian", data: utf16Document("UTF-16", "Anaïs", binary.BigEndian, false), charset: "UTF-16", want: "Anaïs"},
		{name: "invalid UTF-8 default", data: document("", "Ana\xefs"), err: "invalid UTF-8"},
		{name: "incorrect UTF-8 declaration", data: document("UTF-8", "Ana\xefs"), err: "invalid UTF-8"},
		{name: "incorrect UTF-8 transport", data: document("ISO-8859-1", "Ana\xefs"), charset: "UTF-8", err: "invalid UTF-8"},
		{name: "incorrect UTF-8 BOM", data: utf8BOM(document("ISO-8859-1", "Ana\xefs")), charset: "ISO-8859-1", err: "invalid UTF-8"},
		{name: "unsupported transport charset", data: document("UTF-8", "Nin"), charset: "unknown", err: "unsupported XML charset"},
		{name: "unsupported declaration", data: document("unknown", "Nin"), err: "unsupported XML charset"},
		{name: "registered but unsupported charset", data: document("UTF-32", "Nin"), err: "unsupported XML charset"},
		{name: "unsupported UTF-32LE BOM", data: []byte{0xff, 0xfe, 0x00, 0x00}, charset: "UTF-8", err: "unsupported XML BOM"},
		{name: "unsupported UTF-32BE BOM", data: []byte{0x00, 0x00, 0xfe, 0xff}, err: "unsupported XML BOM"},
		{name: "malformed XML remains an error", data: []byte("<document><title>Nin</document>"), charset: "ISO-8859-1", err: "XML syntax error"},
		{name: "HTML entity remains an error", data: document("UTF-8", "&copy;"), err: "invalid character entity"},
		{name: "XML numeric entity", data: document("UTF-8", "&#169;"), want: "©"},
		{name: "empty body", err: "EOF"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got struct {
				Title string `xml:"title"`
			}
			err := UnmarshalXML(tt.data, tt.charset, &got)
			if tt.err != "" {
				require.ErrorContains(t, err, tt.err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got.Title)
		})
	}
}

func TestUnmarshalXMLPreservesSyntaxError(t *testing.T) {
	var got struct{}
	err := UnmarshalXML([]byte("<document>"), "", &got)
	var syntaxError *xml.SyntaxError
	assert.ErrorAs(t, err, &syntaxError)
}
