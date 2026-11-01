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
identifiers, pickup codes, or lending policies.

## Built-in profiles

The directory's [OpenAPI schema](api.yaml) defines vendor defaults in
`x-host-profiles`. This is the sole source of profile values. Each profile uses
the directory's `lmsConfig` and `catalogConfig` structure; `illConfig` remains
independent and is not part of a host profile.

Directory's existing API generation embeds the complete specification, including
`x-host-profiles`, in its Go package. Broker reads that bundled JSON through
`dirapi.GetSpecJSON()` on the first profile resolution, validates definitions
against the generated Directory API types, and caches the definitions or error
for the lifetime of the process. Invalid definitions return a resolution error;
they do not silently fall back to Generic. No separate profile generation step
or generated profile table is needed.

Changing a profile requires regenerating Directory sources and rebuilding broker,
not migrating Directory records. Broker generation prepares Directory sources;
Docker also generates them inside the build. Broker does not parse profile YAML
or fetch profiles over HTTP at runtime. WMS and Aleph remain unsupported.

Conditional catalog defaults (query language, parser replacement, and record
syntax based on the effective parser) remain in Go. Resolution retains value
origins and applies explicit directory overrides without modifying the cached
defaults. Loading bundled definitions preserves existing resolution behavior.

| Profile | Catalog defaults | Circulation defaults |
| --- | --- | --- |
| Generic | Existing MARC parser (852: location b, shelving c, call number h, item p, restricted r); existing PQF or configured CQL queries | NCIP 2: Page, auto scope (Item when an item ID is supplied, otherwise bibItemScopeName, defaulting to Bibliographic Item), pickup enabled |
| Alma | OPAC; nonempty local location and availableNow value 1; item ID and availableThru loan policy | NCIP 2; Item scope |
| Sierra | OPAC; publicNote exactly AVAILABLE, CHECK SHELVES, or CHECK SHELF; localLocation supplies both location and shelving; no item ID or loan policy | NCIP 2; Hold, Title scope; bibItemScopeName Title; Sierra bib-ID normalization |
| Koha | MARC holdings (ZOOM requests `usmarc`, converted to XML for parsing); one candidate per 952; b location, c shelving, o call number; 7 must equal 0 and q must be absent; no item ID | NCIP 2 with XML namespace disabled; Item scope |
| FOLIO | Alma OPAC mappings plus temporaryLocation as temporary shelving location | NCIP 2; Page; Item scope (Title can be configured); bibItemScopeName Title; pickup enabled when a location is supplied |
| Evergreen | MARC 852: b location, a shelving, c call number, g item; n must equal Available; identifier index 1016 | NCIP 2; Loan, Bibliographic Item scope, pickup disabled; BibID code |
| Horizon | OPAC: nonempty location, availableNow=1, Internet locations excluded; one candidate per nonempty shelvingData/temporaryLocation loan policy; trim whitespace | NCIP 1; Loan, Bibliographic Item scope, pickup disabled |
| Polaris | MARC 852: b location, a shelving, h call number, p item; r must equal In; no restricted subfield | NCIP 1; Loan, Bibliographic Item scope, pickup disabled |
| Symphony | MARC 926: a location, b shelving, c call number, d loan policy; b required and must not be RESERVES/CHECKEDOUT/MISSING/DISCARD; identifier index 1016 | NCIP 1; Loan, Bibliographic Item scope, pickup disabled; strip leading non-digits from bib ID |


WMS and Aleph are reserved enum values for follow-up work. Selecting either
produces a broker validation error identifying the unsupported profile.

Vendor profiles use the existing PQF indexes: identifier 12 (1016 for Evergreen and Symphony),
ISBN 7, ISSN 8, title 4. Explicit `queryConfig.type: cql` selects the existing CQL
mappings (`rec.id`, `isbn`, `issn`, `title`). Switching query language discards inherited templates in the previous language.
Each query template can be overridden;
an empty template disables that field. Metadata uses the existing MARC mappings
and can extract the bibliographic MARC record embedded in OPAC.

Record defaults follow the effective holdings parser, including for Generic:

| Holdings parser | Native ZOOM `preferredRecordSyntax` | SRU `recordSchema` |
| --- | --- | --- |
| `marc` (including Koha) | `usmarc` | `marcxml` |
| `opac` | `opac` | `opac` |
| `reservoir` | `usmarc` | `marcxml` |
| `marc21plus1` | `xml` | `marcxml` |

For Koha, native ZOOM requests USMARC over Z39.50 and converts the returned records
to MARCXML before parsing the holdings. The metaproxy adapter requests SRU schema
`opac` when its ZOOM settings select OPAC, and `marcxml` otherwise; backend syntax
conversion is controlled by the proxy configuration. Changing the holdings parser
changes the default syntax to match. Explicit syntax/schema settings take precedence.

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
any occurrence, including an empty subfield. `present` requires an occurrence,
including an empty one. `notIn` requires a `values` array and rejects a candidate
if any occurrence matches an excluded value; use it together with `present` to
reject missing subfields. Comparisons are exact and untrimmed. An empty `values`
array excludes nothing. `itemLoanPolicySubField` maps the MARC loan policy.
Koha's default is:

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
- `includeItemId`, `includeItemLoanPolicy`: circulation mappings.
- `includeTemporaryLocation`: use a nonempty, trimmed circulation temporary location as the effective shelving location for holdings policy and supplier ordering; otherwise retain the permanent shelving location. Enabled by default for FOLIO.
- `allCirculations` (deprecated): accepted and preserved for compatibility, but ignored. All available circulation candidates are returned regardless of this value. It can be omitted from new configurations and imports.
- `excludedLocations`: reject holdings whose trimmed local location exactly matches an entry; `[]` clears exclusions. Filtering applies per holding, preserving other holdings and records.
- `itemLoanPolicySources`: ordered list of `availableThru`, `shelvingData`, or `temporaryLocation`. Each nonempty trimmed value produces a separate candidate, even when source values coincide. Omission uses `availableThru`; `[]` extracts no policy. `includeItemLoanPolicy: false` disables extraction.
- `requireItemLoanPolicy`: exclude candidates without a policy (default false). Horizon enables this, so available circulations with no configured policy value are omitted. It also applies when policy extraction is disabled. The public-note availability mode does not extract circulation policies.
- `trimWhitespace`: trim location, shelving location, call number and item ID (default false; enabled for Horizon).

OPAC parsing returns all technically available circulations. Generic OPAC does
not require a local location; Alma and FOLIO require a nonempty local location. Sierra evaluates each holding's
public note even when no circulation elements are present.

LMS overrides include existing operation enablement, RequestItem type, scope,
bib-ID code and pickup behavior, plus `ncipVersion`, `ncipNamespaceEnabled` and
`bibIdNormalization` (`none`, `sierra`, or `stripLeadingNonDigits`). Sierra normalization strips a leading
`.b` and removes a trailing digit when more than one character remains, matching
mod-rs. Set `none` for an endpoint accepting the original bibliographic ID.
`stripLeadingNonDigits` removes everything before the first ASCII digit and leaves
identifiers without digits unchanged. It works with either NCIP version.

Catalog identifier handling is independent: `queryConfig.identifierNormalization`
accepts `none` or `stripLeadingLetters` (ASCII letters, retaining an all-letter
identifier), followed by `queryConfig.identifierPrefix`. Symphony sets these to
`stripLeadingLetters` and `^C`, giving `a12345` → `^C12345`. Circulation instead
maps `.b12345` → `12345`. Transformations affect only nonempty identifier searches,
before PQF/CQL escaping; ISBN, ISSN, title and original record identifiers are
unchanged. Set normalization to `none` and prefix to `""` to disable both.

The new profiles use existing cancellation workflows: supplier cannot-supply,
accepted cancellation, and retry paths cancel stored LMS request IDs. No vendor
cancellation switch is introduced. TLC and Voyager remain deferred; NCIP transport
is still HTTP(S), independently of the configured version.

## Persistence, diagnostics, and migration

Directory GET responses represent administrator overrides. The API does not
materialize LMS or query defaults on POST/PATCH. Omitted settings inherit at
adapter creation; default corrections therefore take effect without rewriting
entries. PATCH a profile to null to clear its explicit selection. The new LMS
namespace and normalization overrides also support null to resume inheritance.
Partial holdings PATCHes merge stored overrides for the same parser and replace
them when switching parser.

Migration `009_host_profiles` adds nullable profile/protocol columns and a JSON
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

### NCIP version selection

Set `lmsConfig.ncipVersion` to `"1.0"` or `"2.02"`. Horizon, Polaris and Symphony
default to `"1.0"`; other profiles default to `"2.02"`. An explicit value overrides
the profile. Existing profiles retain their prior defaults without a stored version. POST/GET preserve explicit settings;
PATCH with `null` removes the override, while omission leaves it unchanged.
`ncipNamespaceEnabled` affects v2 only: v1 always uses namespace-free XML.

```json
{"lmsConfig":{"ncipVersion":"1.0"}}
```

NCIP 1.0 currently supports HTTP. Its supported operations and differences are
listed in the [adapter documentation](../broker/ncipclientv1/README.md).
Apply Directory migration `020_ncip_version` before running the updated
Directory service. Enable v1 after the updated broker is deployed; older
brokers do not interpret the version field.

### Additional profile deployment

Apply Directory migration `021_catalog_identifier_options` before deploying the
updated Directory service. It adds nullable query normalization/prefix columns
without rewriting records; holdings options use the existing JSON configuration.
Deploy the updated broker before selecting the new profiles or options: older
brokers reject the new profiles and do not interpret the new options. Existing
profiles, endpoints, credentials and overrides are preserved.

All additive import fields are optional for compatibility with older complete
imports. Omitted or null new fields in a complete import clear stored overrides;
PATCH omission preserves them. Query PATCHes follow existing query-field semantics:
use `none` and an empty prefix to disable transforms. Empty holdings arrays and
explicit false values override profile defaults.

### Publication metadata extraction

`catalogConfig.metadataFormat.marc21` accepts `publisher`, `publicationDate`, and
`placeOfPublication` extraction specifications, in addition to the existing
bibliographic fields. Omitted values use broker defaults: `264*1$b/260$b`,
`264*1$c/260$c`, and `264*1$a/260$a`, respectively. The asterisk in the first indicator in
these selectors matches any first indicator; the second indicator must be `1`
(publication). An empty string disables extraction. Publication dates remain
catalog text. These optional settings are supported by create, patch, reads, and
imports; existing import payloads remain valid. Directory migration 023 adds
nullable columns without rewriting existing data. Deploy the Directory migration
and service before configuring the new fields.

### Automatic NCIP request scope

With `lmsConfig.requestItemRequestScopeType: auto`, a supplied item ID selects
`Item` scope. Otherwise the broker uses `lmsConfig.bibItemScopeName`, defaulting
in code to `Bibliographic Item`. Only Sierra and FOLIO profiles override that
name to `Title`. Library overrides take precedence over the profile. Explicit
request scopes retain their existing behavior; changing `bibItemScopeName`
does not enable `auto` or change an explicit scope. Both NCIP versions send
the configured scope name unchanged.

The optional `bibItemScopeName` setting is stored as an override. PATCH with
`null` clears it and resumes profile inheritance; omission preserves it.
Apply Directory migration `024_bib_item_scope_name` before running the updated
Directory service. Existing rows remain unset and inherit their profile values.
