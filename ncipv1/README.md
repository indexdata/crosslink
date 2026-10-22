# NCIP v1.0 XML types

Module `github.com/indexdata/crosslink/ncipv1` provides generated NCIP v1.0
messages for use with `encoding/xml`. The sibling `ncip` module provides v2.
This module contains protocol types; HTTP/socket transport and broker integration
are separate work.

## Generation and checks

Requires Go (see `go.mod`), `make`, and `xsltproc`:

```sh
make -C ncipv1 generate
make -C ncipv1 check
make -C ncipv1 lint
make -C ncipv1 vulncheck
```

The module pins `xsd2goxsl` as a Go tool and generates `ncip1_gen.go` from the
vendored XSD. Generated Go is ignored by Git; edit the schema or generation
options and regenerate. Generation does not fetch the schema from the network.

The schema has no target namespace, so messages use unqualified elements and
attributes. Set `NCIPMessage.Version` to `NCIP_V1_0_DTD` for the v1.0 wire format
used by mod-rs. The version attribute identifies a DTD even though the Go types
are generated from XSD. Dates use `utils.XSDDateTime`; scheme/value pairs are
nested elements. `encoding/xml` does not enforce XSD choice or required-field
constraints; callers must populate the appropriate message and required fields.

## Schema provenance

`xsd/ncip_v1_0.xsd` is the approved version dated 16 August 2002, retrieved on
22 October 2026 from an [archived NISO download](https://web.archive.org/web/20170825233147id_/http://www.niso.org/schemas/ncip/ncip_v1_0.xsd).
The live NISO link returned 404 and the maintenance agency's mirror timed out.
The vendored file is unchanged, including its original whitespace and history.
SHA-256: `3b621d2aee697e8f96add65cc9eef4fd19f4a593d9fee2364085621c98a76a22`.

The checkout and checkin-error fixtures come from
`openlibraryenvironment/lib-ncip-client` v0.24.4
(`ncipOneCheckoutItemXmlResponseExample.xml` and
`ncipOneCheckinItemXmlErrorResponseExample.xml`), with a final newline added.
The lookup request fixture exercises repeated user-element selectors and omitted
optional fields. Tests verify serialization, response values, dates, nested
processing errors, and XML round trips.

Fixtures can also be checked against the source schema with libxml2:

```sh
xmllint --nonet --noout --schema ncipv1/xsd/ncip_v1_0.xsd ncipv1/testdata/*.xml
```
