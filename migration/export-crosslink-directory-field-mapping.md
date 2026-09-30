# mod-rs to CrossLink Directory Field Mapping

This document describes how `export-crosslink-directory.sql` converts a
mod-rs tenant directory into CrossLink directory-import NDJSON. It covers
directory entries, symbols, endpoints, addresses, tenant-local integration
settings, holdings policies, generated tiers, and the generated network.

CrossLink field paths below are relative to the relevant NDJSON record. For
application settings, the exporter uses the trimmed `st_value`, falling back to
the trimmed `st_default_value` when necessary.

## Directory entries

| mod-rs source | CrossLink field | Mapping |
| --- | --- | --- |
| `directory_entry.de_name` | `data.name` | Copied directly. |
| `refdata_value.rdv_value` through `directory_entry.de_type_rv_fk` | `data.type` | `consortium` becomes `Consortium`; `institution` becomes `Institution`; `branch` becomes `Branch`. Other values are rejected. |
| `directory_entry.de_parent` | `data.parent` | The parent database ID is replaced by the parent entry's `{authority, symbol}` key. Root entries receive `null`. |
| `directory_entry.de_desc` | `data.description` | Copied directly. |
| `directory_entry.de_contact_name` | `data.contactName` | Copied directly. |
| `directory_entry.de_email_address` | `data.email` | Copied directly. |
| `directory_entry.de_phone_number` | `data.phoneNumber` | Copied directly. |
| `directory_entry.de_lms_location_code` | `data.lmsLocationCode` | Copied directly. |
| Entry matched by the psql `owner` variable | `data.vendor` | Set to `ReShare` for the matched local entry; otherwise `null`. |
| `directory_entry_tag` joined to `tag.norm_value` | Record inclusion | Entries tagged `deleted`, case-insensitively, are excluded. |

The record discriminator is always `type: entry`. Parent records are emitted
before their children.

## Symbols and entry keys

| mod-rs source | CrossLink field | Mapping |
| --- | --- | --- |
| `naming_authority.na_symbol` | `data.symbols[].authority` | Trimmed and converted to uppercase. Blank values are excluded. |
| `symbol.sym_symbol` | `data.symbols[].symbol` | Trimmed and converted to uppercase. Blank values and values beginning with `DELETED-` are excluded. |
| First symbol ordered by `symbol.sym_priority`, authority, symbol, and `symbol.sym_id` | `key.authority`, `key.symbol` | Used as the stable CrossLink import key. |
| Entry with no usable symbol | `key` and `data.symbols[0]` | Authority is `ISIL`; symbol is the trimmed, uppercased entry name with spaces replaced by hyphens. |
| `symbol.sym_owner_fk` | Symbol ownership | Associates each exported symbol with its directory entry. |

The exporter rejects duplicate normalized `{authority, symbol}` combinations.

## Endpoints

| mod-rs source | CrossLink field | Mapping |
| --- | --- | --- |
| `service.se_name` | `data.endpoints[].name` | Copied directly. |
| `refdata_value.rdv_value` through `service.se_type_fk` | `data.endpoints[].type` | Copied directly. |
| `service.se_address` | `data.endpoints[].address` | Copied directly. |
| `service_account.sa_account_holder` | Endpoint ownership | Associates the service with its directory entry. |
| `service_account.sa_service` | Endpoint service | Joins the account to the service record. |

Duplicate endpoint objects are removed. Endpoint name, type, and address must
all be nonblank.

## Addresses

| mod-rs source | CrossLink field | Mapping |
| --- | --- | --- |
| `address.addr_label` | `data.addresses[].type` | `default`, `shipping`, `billing`, and `other` become `Default`, `Shipping`, `Billing`, and `Other`. Any other value also becomes `Other`. |
| `address_line.al_seq` | `data.addresses[].addressComponents[].seq` | Cast to an integer. |
| Address-line `refdata_value.rdv_value` through `address_line.al_type_rv_fk` | `data.addresses[].addressComponents[].type` | `thoroughfare`, `locality`, `administrativearea`, `postalcode`, and `countrycode` become the corresponding title-cased CrossLink values. Other values become `Other`. |
| `address_line.al_value` | `data.addresses[].addressComponents[].value` | Copied directly. |
| `address.addr_country_code` | Additional `addressComponents[]` item | Appended as a `CountryCode` component after the existing components when no country-code address line already exists. |
| `address.owner_id` | Address ownership | Associates the address with its directory entry. |

## Local entry selection

Tenant-local catalog and holdings settings are attached only to the entry
selected by the psql `owner` variable. Pass it as
`--set=owner=AUTHORITY:SYMBOL`; the value is matched to the normalized
`<authority>:<symbol>` form. The exporter requires exactly one matching entry.
LMS, catalog, ILL, and holdings configuration are attached only to that
selected local entry when their respective configuration data is present.

The `include_consortium` psql parameter controls whether the consortium entry
is emitted and defaults to `true`. When set to `false`, the owner's parent
consortium reference is omitted from the remaining entry records.

The `include_tiers_network` psql parameter controls whether generated tiers
and the network are emitted and defaults to `true`. For multi-schema exports,
the wrapper emits the consortium entry in the first shard and tier/network
templates in the final shard. The join script consolidates all shard entry
keys into those final tier and network records.

## LMS and NCIP configuration

`data.lmsConfig` is emitted only for the selected local entry when both
`ncip_server_address` and `ncip_from_agency` are present.

| mod-rs source | CrossLink field | Mapping |
| --- | --- | --- |
| `app_setting.host_lms_integration` | `data.lmsConfig.vendor` | `alma` becomes `Alma`; `sierra` becomes `Sierra`; `koha` becomes `Koha`; `folio` becomes `FOLIO`; `wms` and `wms2` become `WMS`; `aleph` becomes `Aleph`; any other or missing value becomes `Generic`. |
| `app_setting.ncip_server_address` | `data.lmsConfig.address` | Copied directly. |
| `app_setting.ncip_from_agency` | `data.lmsConfig.fromAgency` | Copied directly. |
| `app_setting.ncip_from_agency_authentication` | `data.lmsConfig.fromAgencyAuthentication` | Copied directly. This value can contain credentials and must be handled as sensitive migration data. |
| `app_setting.ncip_to_agency` | `data.lmsConfig.toAgency` | Copied directly. |
| `app_setting.borrower_check` | `data.lmsConfig.lookupUserEnabled` | `true` when the value equals `ncip`, case-insensitively; otherwise `false`. |
| `app_setting.accept_item` | `data.lmsConfig.acceptItemEnabled` | `true` when the value equals `ncip`, case-insensitively; otherwise `false`. |
| `app_setting.check_in_item` | `data.lmsConfig.checkInItemEnabled` | `true` when the value equals `ncip`, case-insensitively; otherwise `false`. |
| `app_setting.check_out_item` | `data.lmsConfig.checkOutItemEnabled` | `true` when the value equals `ncip`, case-insensitively; otherwise `false`. |
| `app_setting.use_request_item` | `data.lmsConfig.requestItemEnabled` | `true` when the value equals `ncip`, case-insensitively; otherwise `false`. |
| `directory_entry.de_lms_location_code` | `data.lmsConfig.requestItemPickupLocationEnabled` | `true` when the value is not `null`; otherwise `false`. |
| `directory_entry.de_lms_location_code` | `data.lmsConfig.requesterPickupLocation` | Copied directly. |
| `app_setting.ncip_request_item_pickup_location` | `data.lmsConfig.supplierPickupLocation` | Copied directly. |
| Local entry custom property `folio_location_filter` | `data.lmsConfig.itemLocation` | Copied when present; otherwise `null`. mod-rs passes this value as the item location in NCIP RequestItem messages. |
| `app_setting.host_lms_integration` and host-LMS adapter behavior | `data.lmsConfig.requestItemRequestType` | `sierra` becomes `Hold`; `folio` becomes `Page`; all other values become `Loan`. |
| `app_setting.host_lms_integration` and `app_setting.ncip_use_title_request_type` | `data.lmsConfig.requestItemRequestScopeType` | `sierra` becomes `Title`; `folio` becomes `Title` when the setting is `yes` and `Item` otherwise; all other values become `Bibliographic Item`. |
| `app_setting.host_lms_integration` and host-LMS adapter behavior | `data.lmsConfig.requestItemBibIdCode` | `evergreen` becomes `BibID`; all other values become `SYSNUMBER`. |
| `app_setting.default_institutional_patron_id` | `data.lmsConfig.requesterPatronPattern` | Copied as a literal fallback patron identifier. Per-requester `local_institutionalPatronId` directory overrides cannot be represented by a single CrossLink pattern and are not exported. |
| Visible `host_lms_patron_profile` rows | `data.lmsConfig.patronProfiles[]` | Exports `hlpp_code`, `hlpp_name`, and `hlpp_can_create_requests`. A null `hlpp_can_create_requests` becomes `true`, matching mod-rs behavior; hidden rows are excluded. |

The presence of dependent NCIP settings without both
`ncip_server_address` and `ncip_from_agency` causes the export to fail.

## Catalog configuration

`data.catalogConfig` is emitted only for the selected local entry when
`z3950_server_address` is present.

| mod-rs source | CrossLink field | Mapping |
| --- | --- | --- |
| `app_setting.host_lms_integration` | `data.catalogConfig.profile` | Uses the same vendor conversion as `data.lmsConfig.vendor`. |
| `app_setting.z3950_server_address` | `data.catalogConfig.zoom.address` | Copied directly. |

The legacy HTTP Z39.50 proxy is deployment-level CrossLink configuration and
is not exported.

## ILL configuration

`data.illConfig` is emitted for the entry selected by the `owner` variable even
when no ISO18626 endpoint is configured. It is also emitted for non-local
entries tagged as pickup locations so that `isPickupLocation` is preserved.
The owner's direct branches inherit the tenant policy values when they are
exported as pickup locations. Other non-local pickup entries have `null` for
the ISO18626 endpoint, vendor, and tenant policy values; the remaining fields
retain their exported defaults.

| mod-rs source | CrossLink field | Mapping |
| --- | --- | --- |
| First `service.se_address` whose normalized service type is `ISO18626` | `data.illConfig.iso18626Url` | The first matching endpoint ordered by `service.se_id` is used. |
| Presence of the ISO18626 endpoint | `data.illConfig.iso18626Vendor` | Set to `ReShare`. |
| `app_setting.max_requests` | `data.illConfig.maxRequestsPerPatron` | Cast to an integer for the selected local entry and its direct branches. Values must be from 0 through 2147483647; the value is `null` for other non-local pickup entries. |
| `app_setting.check_duplicate_time` | `data.illConfig.duplicateCheckWindowHours` | Cast to an integer number of hours for the selected local entry and its direct branches. Values must be from 0 through 2147483647; the value is `null` for other non-local pickup entries. |
| Unsupported mod-rs agency-information behavior | `data.illConfig.includeRequestingAgencyInfo` | Set to `false`; mod-rs does not populate requesting-agency information. |
| Unsupported mod-rs agency-information behavior | `data.illConfig.includeSupplierInfo` | Set to `false`; mod-rs does not populate supplier information. |
| Unsupported mod-rs directory-derived return information | `data.illConfig.includeReturnInfo` | Set to `false`; mod-rs does not automatically populate return information from the supplier directory entry. |
| Unsupported mod-rs vendor-note behavior | `data.illConfig.includeVendorNote` | Set to `false`; mod-rs does not prepend the supplier vendor to generated notes. |

`app_setting.last_resort_lenders` is the mod-rs source for
`data.illConfig.lendersOfLastResort`, but the primary export emits an empty
array. CrossLink imports NDJSON records sequentially, so a local parent entry
cannot reference child entries that have not yet been imported. Parse and
configure the setting in a second import or post-import update after all
directory entries exist.

## Holdings policy

`data.holdingsPolicy` is attached only to the selected local entry.

| mod-rs source | CrossLink field | Mapping |
| --- | --- | --- |
| `host_lms_location.hll_code` | `data.holdingsPolicy.locations[].code` | Copied directly. |
| `host_lms_location.hll_name` | `data.holdingsPolicy.locations[].name` | Uses the name when present; otherwise falls back to `hll_code`. |
| `host_lms_location.hll_supply_preference` | `data.holdingsPolicy.locations[].supplyPreference` | Negative values become `-1`; `null` becomes `0`; other values are cast to integers. |
| `host_lms_location.hll_hidden` | Location inclusion | Hidden locations are excluded. `null` is treated as visible. |
| `host_lms_shelving_loc.hlsl_code` | `data.holdingsPolicy.shelvingLocations[].code` | Copied directly. |
| `host_lms_shelving_loc.hlsl_name` | `data.holdingsPolicy.shelvingLocations[].name` | Uses the name when present; otherwise falls back to `hlsl_code`. |
| `host_lms_shelving_loc.hlsl_supply_preference` | `data.holdingsPolicy.shelvingLocations[].supplyPreference` | Negative values become `-1`; `null` becomes `0`; other values are cast to integers. |
| `host_lms_shelving_loc.hlsl_hidden` | Shelving-location inclusion | Hidden shelving locations are excluded. `null` is treated as visible. |
| Joined `host_lms_location.hll_code` | `data.holdingsPolicy.locationPolicies[].locationCode` | Copied from the location referenced by `shelving_loc_site.sls_location_fk`. |
| Joined `host_lms_shelving_loc.hlsl_code` | `data.holdingsPolicy.locationPolicies[].shelvingLocationCode` | Copied from the shelving location referenced by `shelving_loc_site.sls_shelving_loc_fk`. |
| `shelving_loc_site.sls_supply_preference` | `data.holdingsPolicy.locationPolicies[].supplyPreference` | Negative values become `-1`; `null` becomes `0`; other values are cast to integers. |
| `host_lms_item_loan_policy.hlilp_code` | `data.holdingsPolicy.itemLoanPolicies[].code` | Copied directly. |
| `host_lms_item_loan_policy.hlilp_name` | `data.holdingsPolicy.itemLoanPolicies[].name` | Uses the name when present; otherwise falls back to `hlilp_code`. |
| `host_lms_item_loan_policy.hlilp_lendable` | `data.holdingsPolicy.itemLoanPolicies[].lendable` | Copied directly. |
| `host_lms_item_loan_policy.hlilp_hidden` | Item-loan-policy inclusion | Only rows where this value is exactly `false` are included. |

Visible holdings supply preferences greater than 10000 cause the export to
fail.

## Generated tiers

When `include_tiers_network` is `true`, the exporter creates one default loan
tier and one default copy tier.

| mod-rs source | CrossLink field | Mapping |
| --- | --- | --- |
| Consortium entry's stable key | `key.consortium` | Uses the consortium's `{authority, symbol}` key. Exactly one consortium entry is required. |
| `app_setting.default_service_level` | `key.name` | Produces `Default <level> loan` and `Default <level> copy`. Missing values default to `standard`. |
| `app_setting.default_service_level` | `data.level` | Trimmed and lowercased; missing values default to `standard`. Supported values are `express`, `normal`, `rush`, `secondarymail`, `standard`, and `urgent`. |
| Generated tier kind | `data.type` | One record uses `loan`; the other uses `copy`. |
| `app_setting.minimum_cost` | `data.cost` | Cast to a number; missing values default to `0`. The value must be nonnegative. |
| Every non-consortium entry's stable key | `data.entries[]` | Each institution and branch is added as `{authority, symbol}`. |

The mod-rs automatic-fee `request_service_type` setting is not used because it
does not describe routing capabilities.

## Generated network

When `include_tiers_network` is `true`, the exporter creates one reciprocal
network.

| mod-rs source | CrossLink field | Mapping |
| --- | --- | --- |
| Consortium entry's stable key | `key.consortium` | Uses the consortium's `{authority, symbol}` key. |
| Generated constant | `key.name` | Set to `Default`. |
| Generated constant | `data.reciprocal` | Set to `true`. |
| Every non-consortium entry's stable key | `data.entries[].authority`, `data.entries[].symbol` | Adds each institution and branch to the network. |
| Entry export order | `data.entries[].priority` | Sequential integer priority based on hierarchy depth, entry name, and entry ID. |

## Fixed and profile-resolved fields

The CrossLink import contract also requires fields with no safe mod-rs source,
or fields whose effective value is supplied by CrossLink's selected host-LMS
profile. The exporter supplies the following fixed values.

| CrossLink field | Exported value |
| --- | --- |
| `data.organizationId` | `null` |
| `data.fromEmail` | `null` |
| `data.tenant` | `null` |
| `data.hrid` | `null` |
| `data.timeZone` | `null` |
| `data.closures` | `[]` |
| `data.lmsConfig.ncipNamespaceEnabled` | `null`; resolved by the CrossLink host-LMS profile. |
| `data.lmsConfig.bibIdNormalization` | `null`; resolved by the CrossLink host-LMS profile. |
| `data.catalogConfig.metadataUpdateMode` | `null`; CrossLink consequently does not update request metadata from catalog records. |
| `data.catalogConfig.sru` | `null` |
| `data.catalogConfig.zoom.options` | `null`; resolved from the holdings format by the CrossLink host-LMS profile. |
| `data.catalogConfig.queryConfig` | `null`; resolved by the CrossLink host-LMS profile. |
| `data.catalogConfig.holdingsFormat` | `null`; resolved by the CrossLink host-LMS profile. |
| `data.catalogConfig.metadataFormat` | `null`; resolved by the CrossLink host-LMS profile. |
| `data.illConfig.useOfferedCosts` | `null` |
| `data.illConfig.noteFieldSeparator` | `null` |
| `data.illConfig.supplierPatronPattern` | `null` |

Missing symbols, endpoints, addresses, and holdings-policy collections are
exported as empty arrays. Missing optional configuration objects are exported
as `null`.

## Export constraints

The exporter validates the data before producing NDJSON:

- exactly one consortium entry must exist;
- every exported entry must have a nonblank name and a supported type;
- the hierarchy must be reachable and cycle-free;
- institutions may only be children of a consortium;
- branches must be children of institutions;
- normalized symbols must be unique;
- relevant application settings may have at most one row per key;
- NCIP-dependent configuration requires both the NCIP server address and the
  from-agency value;
- `max_requests` and `check_duplicate_time`, when present, must be integers
  from 0 through 2147483647;
- visible holdings supply preferences must not exceed 10000; and
- each serialized record must not exceed 1 MiB.

The output order is entries, followed by the two generated tiers, followed by
the generated default network.
