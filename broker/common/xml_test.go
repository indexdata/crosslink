package common

import (
	"bytes"
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
	utf16Surrogate := func(unit uint16) []byte {
		data := utf16Document("UTF-16", "X", binary.LittleEndian, true)
		i := bytes.Index(data, []byte{'X', 0})
		require.NotEqual(t, -1, i)
		binary.LittleEndian.PutUint16(data[i:], unit)
		return data
	}
	tests := []struct {
		name    string
		data    []byte
		charset string
		want    string
		err     string
	}{
		{name: "UTF-8 default", data: document("", "Anaïs © — 🙂"), want: "Anaïs © — 🙂"},
		{name: "UTF-8 declaration", data: document("UTF-8", "Götterzorn"), want: "Götterzorn"},
		{name: "declaration without encoding defaults to UTF-8", data: []byte(`<?xml version="1.0"?><document><title>Anaïs</title></document>`), want: "Anaïs"},
		{name: "encoding attribute in document does not affect declaration", data: []byte(`<?xml version="1.0"?><document encoding="windows-1252"><title>Anaïs</title></document>`), want: "Anaïs"},
		{name: "Latin-1 declaration", data: document("ISO-8859-1", "Ana\xefs Nin"), want: "Anaïs Nin"},
		{name: "declaration spaces before equals", data: []byte("<?xml version=\"1.0\" encoding =\"ISO-8859-1\"?><document><title>Ana\xefs</title></document>"), want: "Anaïs"},
		{name: "declaration spaces after equals", data: []byte("<?xml version=\"1.0\" encoding= \"ISO-8859-1\"?><document><title>Ana\xefs</title></document>"), want: "Anaïs"},
		{name: "declaration whitespace and single quotes", data: []byte("<?xml\tversion = '1.0'\r\nencoding\n=\t'LaTiN1' standalone = 'yes' \t?><document><title>Ana\xefs</title></document>"), want: "Anaïs"},
		{name: "declaration exact attribute name", data: []byte(`<?xml version="1.0" extraencoding="windows-1252"?><document><title>Anaïs</title></document>`), want: "Anaïs"},
		{name: "declaration encoding attribute suffix", data: []byte(`<?xml version="1.0" encodingExtra="windows-1252"?><document><title>Anaïs</title></document>`), want: "Anaïs"},
		{name: "declaration ignores encoding text inside another value", data: []byte(`<?xml version="1.0" other='encoding="windows-1252"'?><document><title>Anaïs</title></document>`), want: "Anaïs"},
		{name: "Windows-1252 declaration", data: document("windows-1252", "\x93Nin\x94 \x97 \xa9 \x80"), want: "“Nin” — © €"},
		{name: "Shift-JIS declaration", data: document("Shift_JIS", "\x93\xfa\x96\x7b"), want: "日本"},
		{name: "transport overrides UTF-8 declaration", data: document("UTF-8", "G\xf6tterzorn"), charset: "ISO-8859-1", want: "Götterzorn"},
		{name: "transport without declaration", data: document("", "Ana\xefs Nin"), charset: "ISO-8859-1", want: "Anaïs Nin"},
		{name: "transport overrides legacy declaration", data: document("ISO-8859-1", "\x80\x97"), charset: "windows-1252", want: "€—"},
		{name: "transport conversion happens once", data: document("ISO-8859-1", "Ana\xefs Nin"), charset: "ISO-8859-1", want: "Anaïs Nin"},
		{name: "UTF-8 transport overrides legacy declaration", data: document("ISO-8859-1", "Anaïs © 🙂"), charset: "UTF-8", want: "Anaïs © 🙂"},
		{name: "transport charset alias", data: document("UTF-8", "Ana\xefs"), charset: " LaTiN1 ", want: "Anaïs"},
		{name: "transport overrides unsupported declaration", data: document("unknown", "Anaïs"), charset: "UTF-8", want: "Anaïs"},
		{name: "transport overrides spaced declaration", data: []byte(`<?xml version="1.0" encoding = "unknown"?><document><title>Anaïs</title></document>`), charset: "UTF-8", want: "Anaïs"},
		{name: "UTF-8 BOM overrides transport and declaration", data: utf8BOM(document("ISO-8859-1", "Anaïs © 🙂")), charset: "windows-1252", want: "Anaïs © 🙂"},
		{name: "UTF-8 BOM overrides unsupported charset", data: utf8BOM(document("unknown", "Anaïs")), charset: "unknown", want: "Anaïs"},
		{name: "UTF-16LE BOM overrides transport and declaration", data: utf16Document("ISO-8859-1", "Anaïs 🙂", binary.LittleEndian, true), charset: "UTF-8", want: "Anaïs 🙂"},
		{name: "UTF-16BE BOM overrides unsupported charset", data: utf16Document("unknown", "Götterzorn 🙂", binary.BigEndian, true), charset: "unknown", want: "Götterzorn 🙂"},
		{name: "UTF-16LE transport", data: utf16Document("UTF-8", "Anaïs", binary.LittleEndian, false), charset: "UTF-16LE", want: "Anaïs"},
		{name: "UTF-16BE transport", data: utf16Document("UTF-8", "Götterzorn", binary.BigEndian, false), charset: "UTF-16BE", want: "Götterzorn"},
		{name: "UTF-16 transport without BOM defaults to big endian", data: utf16Document("UTF-16", "Anaïs", binary.BigEndian, false), charset: "UTF-16", want: "Anaïs"},
		{name: "invalid UTF-8 default is replaced", data: document("", "Ana\xefs"), want: "Ana�s"},
		{name: "invalid UTF-8 declaration is replaced", data: document("UTF-8", "Ana\xefs"), want: "Ana�s"},
		{name: "invalid UTF-8 transport is replaced", data: document("ISO-8859-1", "Ana\xefs"), charset: "UTF-8", want: "Ana�s"},
		{name: "invalid UTF-8 BOM is replaced", data: utf8BOM(document("ISO-8859-1", "Ana\xefs")), charset: "ISO-8859-1", want: "Ana�s"},
		{name: "invalid UTF-8 attribute without declaration is replaced", data: []byte("<document note=\"\xff\"><title>Ana\xefs</title></document>"), want: "Ana�s"},
		{name: "unpaired UTF-16 high surrogate is replaced", data: utf16Surrogate(0xd800), want: "�"},
		{name: "unpaired UTF-16 low surrogate is replaced", data: utf16Surrogate(0xdc00), want: "�"},
		{name: "malformed Shift-JIS declaration is replaced", data: document("Shift_JIS", "\x82 "), want: "� "},
		{name: "malformed Shift-JIS transport is replaced", data: document("UTF-8", "\x82 "), charset: "Shift_JIS", want: "� "},
		{name: "legitimate UTF-8 replacement character is preserved", data: document("UTF-8", "Anaïs �"), want: "Anaïs �"},
		{name: "legitimate UTF-16 replacement character is preserved", data: utf16Document("UTF-16", "Anaïs �", binary.LittleEndian, true), want: "Anaïs �"},
		{name: "unsupported transport charset", data: document("UTF-8", "Nin"), charset: "unknown", err: "unsupported XML charset"},
		{name: "unsupported declaration", data: document("unknown", "Nin"), err: "unsupported XML charset"},
		{name: "unsupported declaration with spaces", data: []byte(`<?xml version="1.0" encoding = "unknown"?><document><title>Nin</title></document>`), err: "unsupported XML charset"},
		{name: "registered but unsupported charset", data: document("UTF-32", "Nin"), err: "unsupported XML charset"},
		{name: "unsupported UTF-32LE BOM", data: []byte{0xff, 0xfe, 0x00, 0x00}, charset: "UTF-8", err: "unsupported XML BOM"},
		{name: "unsupported UTF-32BE BOM", data: []byte{0x00, 0x00, 0xfe, 0xff}, err: "unsupported XML BOM"},
		{name: "malformed XML remains an error", data: []byte("<document><title>Nin</document>"), charset: "ISO-8859-1", err: "XML syntax error"},
		{name: "malformed XML after replacement remains an error", data: []byte("<document><title>Ana\xefs</document>"), err: "XML syntax error"},
		{name: "malformed declaration remains an error", data: []byte(`<?xml version="1.0" encoding="UTF-8"`), err: "XML syntax error"},
		{name: "unsupported XML version remains an error", data: []byte(`<?xml version="2.0"?><document/>`), err: "unsupported version"},
		{name: "non-declaration processing instruction", data: []byte(`<?xml-stylesheet href="style.xsl"?><document><title>Anaïs</title></document>`), want: "Anaïs"},
		{name: "non-declaration encoding is ignored", data: []byte(`<?xml-stylesheet encoding="unknown"?><document><title>Anaïs</title></document>`), want: "Anaïs"},
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
