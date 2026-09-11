# Host LMS and catalog profiles

`lmsConfig.vendor` selects circulation defaults. `catalogConfig.profile` selects
catalog query, metadata, holdings and technical availability defaults. Both are
independent of `illConfig.iso18626Vendor`, which selects the ILL/ISO 18626
implementation. The deprecated top-level `vendor` never supplies a host profile.

For LMS settings, the broker applies Generic defaults, then the selected vendor,
then explicit directory values. For catalog settings, it applies Generic defaults,
then `catalogConfig.profile` (or `lmsConfig.vendor` when no catalog profile is set),
then explicit catalog values. Missing or null profiles resolve to Generic, except
that an unset catalog profile inherits the LMS vendor. Setting the catalog profile
to `Generic` explicitly prevents that inheritance.

```yaml
illConfig:
  iso18626Vendor: ReShare
lmsConfig:
  vendor: Sierra
  address: https://library.example.org/ncip
  fromAgency: EXAMPLE
  requestItemPickupLocationEnabled: false
catalogConfig:
  zoom:
    address: z3950.library.example.org:210/catalog
  holdingsFormat:
    opac:
      availablePublicNotes: [AVAILABLE, CHECK SHELVES, CHECK SHELF]
```

A profile alone does not enable an adapter. NCIP requires both `address` and
`fromAgency`; omit both for a catalog-only institution. A partially configured
NCIP connection fails broker validation. Catalog lookup requires an SRU or ZOOM
address. Profiles never supply endpoints, credentials, agencies, databases, patron
identifiers, pickup codes, or lending policies. Existing Generic circulation
fallbacks remain unchanged.

## Built-in profiles

| Profile | Catalog defaults | Circulation defaults |
| --- | --- | --- |
| Generic | Existing MARC parser (852: location b, shelving c, call number h, item p, restricted r); existing PQF or configured CQL queries | Existing NCIP 2 behavior: Page, Item scope, pickup enabled |
| Alma | OPAC; nonempty local location and availableNow value 1; item ID and availableThru loan policy | NCIP 2 |
| Sierra | OPAC; publicNote exactly AVAILABLE, CHECK SHELVES, or CHECK SHELF; localLocation supplies both location and shelving; no item ID or loan policy | NCIP 2; Hold, Title scope; Sierra bib-ID normalization |
| Koha | MARCXML; one candidate per 952; b location, c shelving, o call number; 7 must equal 0 and q must be absent; no item ID | NCIP 2 with XML namespace disabled |
| FOLIO | Alma OPAC mappings plus temporaryLocation as temporary shelving location | NCIP 2; Page; Item scope (Title can be configured); pickup enabled when a location is supplied |

WMS and Aleph are reserved enum values for follow-up work. Selecting either
produces a broker validation error identifying the unsupported profile.

All supported vendor profiles use the existing PQF indexes: identifier 12,
ISBN 7, ISSN 8, title 4. Explicit `queryConfig.type: cql` selects the existing CQL
mappings (`rec.id`, `isbn`, `issn`, `title`). Each query template can be overridden;
an empty template disables that field. Metadata uses the existing MARC mappings
and can extract the bibliographic MARC record embedded in OPAC.

OPAC profiles request ZOOM `preferredRecordSyntax: opac`; Koha requests `xml`
(MARCXML). SRU requests use `opac` or `marcxml`, respectively. Explicit record
syntax/schema settings take precedence. The metaproxy adapter uses the OPAC schema
when its ZOOM settings select OPAC. Changing the holdings parser changes the
default syntax to match; explicit syntax options remain authoritative.

Every returned record within existing adapter limits is processed. Available
holdings are concatenated across records. Lookup stops at the first query with
holdings; otherwise it continues through identifier, ISBN, ISSN and title.
Profiles do not change this sequence or the directory's `holdingsPolicy`.

## Overrides

Explicit values win, including `false`, empty strings where valid, and empty
arrays. Objects merge recursively. Arrays replace their defaults. A partial
`holdingsFormat` using the same parser merges with that profile's mapping and
rules. Selecting a different parser discards the previous parser and its defaults.
Select exactly one of `marc`, `opac`, `reservoir`, or `marc21plus1`.

For example, this keeps Sierra circulation but uses Koha catalog defaults:

```yaml
lmsConfig:
  vendor: Sierra
catalogConfig:
  profile: Koha
  sru:
    address: https://catalog.example.org/sru
  holdingsFormat:
    marc:
      callNumberSubField: h
```

MARC `availability` is an array of predicates that must all pass. `equals`
requires a present subfield with the exact configured value; `absent` rejects
any occurrence, including an empty subfield. Koha's default is:

```yaml
availability:
  - subField: '7'
    operator: equals
    value: '0'
  - subField: q
    operator: absent
```

`availability: []` clears these predicates. The existing `restrictedSubField`
remains available independently. Generic partial MARC mappings retain legacy
behavior: only specified fields are mapped; an empty MARC configuration uses
852 defaults. A custom MARC mapping should provide `mainField`.

OPAC overrides include:

- `availabilityRule`: `availableNow` or `publicNote`.
- `availablePublicNotes`: exact accepted strings; required for `publicNote`.
- `requireLocalLocation`: require a nonempty local location.
- `shelvingLocationSource`: `shelvingLocation` or `localLocation`.
- `includeItemId`, `includeItemLoanPolicy`, `includeTemporaryLocation`: circulation mappings.
- `allCirculations`: emit every available circulation, instead of the first per holding.

Generic OPAC retains its previous first-available-circulation behavior and does
not require a local location. Alma and FOLIO emit every technically available
circulation with a nonempty local location. Sierra evaluates each holding's
public note even when no circulation elements are present.

LMS overrides include existing operation enablement, RequestItem type, scope,
bib-ID code and pickup behavior, plus `ncipNamespaceEnabled` and
`bibIdNormalization` (`none` or `sierra`). Sierra normalization strips a leading
`.b` and removes a trailing digit when more than one character remains, matching
mod-rs. Set `none` for an endpoint accepting the original bibliographic ID.

## Persistence, diagnostics, and migration

Directory GET responses represent administrator overrides. The API does not
materialize LMS or query defaults on POST/PATCH. Omitted settings inherit at
adapter creation; default corrections therefore take effect without rewriting
entries. PATCH a profile to null to clear its explicit selection. The new LMS
namespace and normalization overrides also support null to resume inheritance.
Partial holdings PATCHes merge stored overrides for the same parser and replace
them when switching parser.

Migration `008_host_profiles` adds nullable profile/protocol columns and a JSON
holdings configuration. It preserves existing explicit holdings settings. It does
not select profiles or copy preset values into any row. Existing entries without
host profiles continue to resolve as Generic. Values previously materialized by
older directory versions remain explicit overrides; clear those LMS values with
PATCH null where profile inheritance is desired.

At debug level the broker logs `resolved host profiles`, including effective
behavior settings and a per-field `origins` map (`Generic`, profile name, or
`directory`). Endpoints, credentials, agencies, patrons and local policies are
excluded. The same representation is available internally through
`profiles.Effective.Diagnostics()`.

Generated Go API models and embedded schemas are rebuilt with
`make -C directory generate`; generated files remain ignored according to the
repository convention.
