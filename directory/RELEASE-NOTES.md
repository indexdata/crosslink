# Unreleased

- Added independent host LMS and catalog profiles for Alma, Sierra, Koha, FOLIO
  and Generic. WMS and Aleph are reserved and report unsupported-profile errors.
- Added Sierra OPAC availability, Koha MARC value/absence predicates, namespace-free
  NCIP, and configurable Sierra bib-ID normalization.
- Migration 008 preserves existing holdings configuration and adds nullable profile
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
