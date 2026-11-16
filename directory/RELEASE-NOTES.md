# Unreleased

- Directory entries now inherit configuration through the consortium →
  institution → branch hierarchy. Entry GETs return merged values by default;
  pass `resolve-inherit=false` for stored values only. Inheritance boundaries,
  merge units and per-type editability are declared in `api.yaml`
  (`x-inheritance-root`, `x-editable-on`, `x-merge-unit`, `x-off-value`). See
  [configuration inheritance](README.md#configuration-inheritance).
- Behavior changes: `null` now removes a local value so that an inherited one
  applies, and is accepted for every configuration field, including LMS
  `address`/`fromAgency`, catalog sub-objects and ILL lists. Writes of fields an
  entry type may not set return HTTP 400 (for example, `illConfig.loadBalancingPolicy`
  is consortium-only, and branches may set only pickup codes and `isPickupLocation`).
  Imports report these as per-record errors, and import nulls clear legacy values.
  On PATCH and update imports, resubmitting a value the entry already stores is
  accepted.
  Only consortial administrators may change an entry's `parent` or `type`;
  institutional administrators get HTTP 401 (resending the stored values is still
  accepted). A stored empty `lendersOfLastResort` now reads as `[]`
  instead of being omitted.
- Use `defaultLoanPeriod: 0` and `ncipVersion: "default"` to switch these settings
  off locally. Apply migration `025_inheritance_off_values` first; its down migration
  converts them back to null. Deploy the updated broker, which treats both values as
  unset, together with Directory.
- The migration export no longer copies the tenant's NCIP and ILL settings onto
  branches; branches inherit them. Branch records exported by earlier versions keep
  overriding inherited values until they are re-imported or cleared with PATCH `null`.

- Add configurable Evergreen, Horizon, Polaris and Symphony host profiles, including
  catalog identifier transformations and MARC/OPAC availability and loan-policy
  options. Apply Directory migration `021_catalog_identifier_options` and deploy
  the updated broker before enabling the new profiles. NCIP transport remains HTTP(S).

- Added independent host LMS and catalog profiles for Alma, Sierra, Koha, FOLIO
  and Generic. WMS and Aleph are reserved and report unsupported-profile errors.
- Added Sierra OPAC availability, Koha MARC value/absence predicates, namespace-free
  NCIP, and configurable Sierra bib-ID normalization.
- Migration 009 preserves existing holdings configuration and adds nullable profile
  fields. Entries without profiles retain Generic behavior; no preset values are
  written into directory records.
- LMS `address` and `fromAgency` are optional in directory records so a host vendor
  can be selected without enabling circulation. Both are required to create an
  NCIP adapter. Catalog profiles also require a connection to enable lookup.
- Directory responses now omit unspecified LMS defaults instead of materializing
  them during POST. Omitted query types also remain omitted on POST/PATCH. Broker
  runtime defaults remain unchanged. Existing stored defaults remain overrides;
  clear optional LMS values with PATCH null to inherit a profile's defaults.
- Holdings overrides now merge for the same parser and replace on parser changes.
  Empty absent holdings configurations are omitted from directory responses.

See [host profile documentation](host-profiles.md) for configuration and upgrade details.
