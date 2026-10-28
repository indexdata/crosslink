# CrossLink migration scripts

These scripts export mod-rs data, combine multi-schema exports, and import the
result into CrossLink.

## Prerequisites

- POSIX shell
- PostgreSQL `psql` for export scripts
- `jq` for multi-schema export and directory consolidation
- `curl` for import scripts

Run the commands from the directory where the input and output NDJSON files
should be created unless a path is supplied through an environment variable.

## Recommended workflow

For a multi-schema migration:

```sh
DATABASE_URL='postgres://user:password@host/database' \
./migration/export-crosslink.sh

CROSSLINK_HOST='http://localhost:8086' \
./migration/import-crosslink-directory.sh

CROSSLINK_BROKER_HOST='http://localhost:8081' \
./migration/import-crosslink-patron-requests.sh
```

`export-crosslink.sh` exports every schema and then joins the results, producing
`directories.ndjson`, `config.ndjson`, `unsupported-templates.ndjson`, and a
`patron-requests/` directory of batch files. The unsupported-template file is a
review artifact and must not be passed to the broker import endpoint.

Import the directory data first because configuration and patron requests refer
to directory entries and symbols. The broker import wrapper then imports
`config.ndjson` so templates and scheduled actions exist before it imports any
patron-request batch.

## `export-crosslink.sh`

Runs the directory, configuration, and open patron-request SQL exports once for
every schema in `schema.properties`.

The properties file uses one entry per line:

```text
schema_name=AUTHORITY:OWNER_SYMBOL
```

Blank lines and lines beginning with `#` are ignored. The first schema exports
the consortium entry. Its consortium UUID is then reused by all later
directory exports. The final schema exports the generated tiers and network.

Directory exports include the selected local owner's cancellation reasons,
unfilled reasons (from mod-rs `cannotSupplyReasons`), and loan conditions.
It also exports the selected local owner's `defaultCopyrightType`, resolving the
`default_copyright_type` setting through the `copyrightType` refdata vocabulary.
An unset setting is omitted; a configured value without a unique refdata match
fails the export.
See the [ILL configuration field mappings](export-crosslink-directory-field-mapping.md#ill-configuration)
for source fields, setting and label fallbacks, ordering, and missing-list behavior.

Environment variables:

| Variable | Default | Description |
| --- | --- | --- |
| `DATABASE_URL` | required | PostgreSQL connection string passed to `psql`. |
| `PROPERTIES_FILE` | `migration/schema.properties` | Schema-to-owner mapping file. |
| `DIRECTORY_EXPORT` | `migration/export-crosslink-directory.sql` | Directory export SQL file. |
| `CONFIG_EXPORT` | `migration/export-crosslink-config.sql` | Template and scheduled-action export SQL file. |
| `PATRON_REQUEST_EXPORT` | `migration/export-crosslink-open-patron-requests.sql` | Patron-request export SQL file. |
| `DIRECTORY_PREFIX` | `directories` | Prefix for per-schema directory files. |
| `CONFIG_PREFIX` | `config` | Prefix for per-schema configuration files. |
| `UNSUPPORTED_TEMPLATE_PREFIX` | `unsupported-templates` | Prefix for per-schema template review files. |
| `PATRON_REQUEST_PREFIX` | `patron-request` | Prefix for per-schema patron-request files. |
| `DIRECTORY_OUTPUT` | `directories.ndjson` | Joined directory output. |
| `CONFIG_OUTPUT` | `config.ndjson` | Joined configuration output. |
| `UNSUPPORTED_TEMPLATE_OUTPUT` | `unsupported-templates.ndjson` | Joined review output for templates excluded from import. |
| `PATRON_REQUEST_DIR` | `patron-requests` | Directory for joined patron-request batch files. |
| `PATRON_REQUEST_BATCH_SIZE` | `5000` | Maximum records per patron-request batch file. |
| `MPALS_SCHEMA` | `minitex_mod_rs` | Schema whose directory supplies the institutions without their own tenant (the mpals libraries). Set it to an empty value to skip that export. |

Per-schema output files are named like:

```text
directories-<schema>-<number>.ndjson
directories-mpals-<number>.ndjson
config-<schema>-<number>.ndjson
unsupported-templates-<schema>-<number>.ndjson
patron-request-<schema>-<number>.ndjson
```

After the tenant schemas, the script exports the institutions without a
tenant from `MPALS_SCHEMA` with `mode=mpals`, passing every owner symbol from
the properties file as `tenant_symbols`. Each such institution gets a tenant
made from its ISIL symbol, for example `mnaleti` for `ISIL:US-MNALETI`. See
`export-crosslink-directory-field-mapping.md`.

Existing files matching the configured prefixes are removed before export.
After all schemas are exported, the script runs `join-crosslink-exports.sh` on
the current directory.

Known Handlebars placeholders, `#if` blocks, and triple-brace placeholders are
converted to CrossLink Go-template syntax. Converted `#if` blocks compare their
field against the `n/a` missing-value sentinel, so absent optional values follow
the Handlebars `else` branch. Triple-brace values use CrossLink's normal HTML
escaping. A converted template is included in `config.ndjson` only when it is
valid for either patron-request or batch-email data according to the exporter's
syntax and data-context checks. Templates with residual Handlebars actions or a
mixture of both data contexts are preserved in `unsupported-templates.ndjson`
with their source, converted content, and review reason.

A `PrintPullSlips` schedule selects a migrated template only when its converted
subject and body are importable and compatible with batch-email data. Otherwise,
the schedule uses the generated `pullslip-email` fallback template.

## `join-crosslink-exports.sh`

Combines the per-schema NDJSON files produced by `export-crosslink.sh`.
`export-crosslink.sh` runs it automatically; run it directly only to re-join
existing shards.

For directory records it also:

- combines all entry records;
- assigns the first exported consortium to root institutions without a parent;
- combines tier and network metadata;
- assigns all non-consortium entries to generated tiers;
- assigns sequential network priorities.

Configuration and unsupported-template review files are concatenated in numeric
export order into `CONFIG_OUTPUT` and `UNSUPPORTED_TEMPLATE_OUTPUT`,
respectively. Patron-request files are concatenated in numeric export order and
written to `PATRON_REQUEST_DIR` as `patron-requests-0001.ndjson`,
`patron-requests-0002.ndjson`, and so on, each holding at most
`PATRON_REQUEST_BATCH_SIZE` records. Existing `patron-requests-*.ndjson` files
in that directory are removed first.

Usage:

```sh
./migration/join-crosslink-exports.sh [SOURCE_DIR]
```

Environment variables:

| Variable | Default | Description |
| --- | --- | --- |
| `DIRECTORY_OUTPUT` | `<SOURCE_DIR>/directories.ndjson` | Joined directory output. |
| `DIRECTORY_PREFIX` | `directories` | Prefix used to find directory shards. |
| `CONFIG_OUTPUT` | `<SOURCE_DIR>/config.ndjson` | Joined configuration output. |
| `CONFIG_PREFIX` | `config` | Prefix used to find configuration shards. |
| `UNSUPPORTED_TEMPLATE_OUTPUT` | `<SOURCE_DIR>/unsupported-templates.ndjson` | Joined template review output. |
| `UNSUPPORTED_TEMPLATE_PREFIX` | `unsupported-templates` | Prefix used to find template review shards. |
| `PATRON_REQUEST_PREFIX` | `patron-request` | Prefix used to find patron-request shards. |
| `PATRON_REQUEST_DIR` | `<SOURCE_DIR>/patron-requests` | Directory for patron-request batch files. |
| `PATRON_REQUEST_BATCH_SIZE` | `5000` | Maximum records per patron-request batch file. |

The script requires at least one directory, configuration, and patron-request
shard. Template review shards are optional so exports created before this review
output was added can still be re-joined; their joined review output is empty.

## `import-crosslink-directory.sh`

POSTs a directory NDJSON file to the CrossLink directory import endpoint.

Environment variables:

| Variable | Default | Description |
| --- | --- | --- |
| `CROSSLINK_HOST` | required | CrossLink directory base URL, for example `http://localhost:8086`. |
| `DIRECTORY_FILE` | `directories.ndjson` | Directory NDJSON file to upload. |
| `CONFLICT_POLICY` | `fail` | Import conflict policy passed as the query parameter. |

Example:

```sh
CROSSLINK_HOST='http://localhost:8086' \
DIRECTORY_FILE=directories.ndjson \
CONFLICT_POLICY=fail \
./migration/import-crosslink-directory.sh
```

The request includes the `directory.consortium.all` permission header. The
script prints the API response and exits nonzero if any of
`.entries.failed`, `.tiers.failed`, or `.networks.failed` is nonzero. Invalid
JSON responses also cause a failure.

## `import-crosslink-patron-requests.sh`

POSTs a non-empty combined `CONFIG_FILE` to the CrossLink broker import
endpoint, then POSTs each `patron-requests-*.ndjson` batch file in
`PATRON_REQUEST_DIR`, in name order. An existing empty configuration file is a
successful no-op; otherwise configuration must succeed before any patron
requests are sent. When the directory contains no batch files, the script
imports non-empty configuration and then exits successfully.

Environment variables:

| Variable | Default | Description |
| --- | --- | --- |
| `CROSSLINK_BROKER_HOST` | required | CrossLink broker base URL, for example `http://localhost:8081`. |
| `CONFIG_FILE` | `config.ndjson` | Combined configuration file uploaded before patron requests when non-empty. |
| `PATRON_REQUEST_DIR` | `patron-requests` | Directory of patron-request batch files to upload. |
| `PATRON_REQUEST_FILE` | unset | Upload only this patron-request NDJSON file instead of the directory, after configuration. |
| `CONFLICT_POLICY` | `fail` | Import conflict policy passed as the query parameter. |

Example:

```sh
CROSSLINK_BROKER_HOST='http://localhost:8081' \
PATRON_REQUEST_DIR=patron-requests \
CONFLICT_POLICY=fail \
./migration/import-crosslink-patron-requests.sh
```

The script preflights the configuration file, patron-request directory, or
explicit patron-request file before making any request. An existing empty
patron-request directory is valid for a configuration-only import. It prints
each API response and stops with a nonzero exit at the first file whose response
has a nonzero `.patronRequests.failed`,
`.batchActions.failed`, or `.templates.failed`. Invalid JSON responses also
cause a failure. A configuration failure prevents all patron-request imports.
Earlier successful files remain imported, so re-run with
`CONFLICT_POLICY=skip` or `update` to continue, or use `PATRON_REQUEST_FILE` to
retry a single batch. Retry runs import a non-empty `CONFIG_FILE` again using
the selected conflict policy before the patron-request file.

All import scripts use `curl --fail-with-body`, so HTTP failures return a
nonzero exit status while preserving the server response body in the output.
