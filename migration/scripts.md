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

./migration/join-crosslink-exports.sh

CROSSLINK_HOST='http://localhost:8086' \
./migration/import-crosslink-directory.sh

CROSSLINK_BROKER_HOST='http://localhost:8081' \
./migration/import-crosslink-patron-requests.sh
```

Import the directory data before patron requests because patron requests refer
to directory entries and symbols.

## `export-crosslink.sh`

Runs the directory and open patron-request SQL exports once for every schema in
`schema.properties`.

The properties file uses one entry per line:

```text
schema_name=AUTHORITY:OWNER_SYMBOL
```

Blank lines and lines beginning with `#` are ignored. The first schema exports
the consortium entry. Its consortium UUID is then reused by all later
directory exports. The final schema exports the generated tiers and network.

Environment variables:

| Variable | Default | Description |
| --- | --- | --- |
| `DATABASE_URL` | required | PostgreSQL connection string passed to `psql`. |
| `PROPERTIES_FILE` | `migration/schema.properties` | Schema-to-owner mapping file. |
| `DIRECTORY_EXPORT` | `migration/export-crosslink-directory.sql` | Directory export SQL file. |
| `PATRON_REQUEST_EXPORT` | `migration/export-crosslink-open-patron-requests.sql` | Patron-request export SQL file. |
| `DIRECTORY_PREFIX` | `directories` | Prefix for per-schema directory files. |
| `PATRON_REQUEST_PREFIX` | `patron-request` | Prefix for per-schema patron-request files. |

Output files are named like:

```text
directories-<schema>-<number>.ndjson
patron-request-<schema>-<number>.ndjson
```

Existing files matching the configured prefixes are removed before export.

## `join-crosslink-exports.sh`

Combines the per-schema NDJSON files produced by `export-crosslink.sh`.

For directory records it also:

- combines all entry records;
- assigns the first exported consortium to root institutions without a parent;
- combines tier and network metadata;
- assigns all non-consortium entries to generated tiers;
- assigns sequential network priorities.

Patron-request files are concatenated in numeric export order.

Usage:

```sh
./migration/join-crosslink-exports.sh [SOURCE_DIR]
```

Environment variables:

| Variable | Default | Description |
| --- | --- | --- |
| `DIRECTORY_OUTPUT` | `<SOURCE_DIR>/directories.ndjson` | Joined directory output. |
| `DIRECTORY_PREFIX` | `directories` | Prefix used to find directory shards. |
| `PATRON_REQUEST_PREFIX` | `patron-request` | Prefix used to find patron-request shards. |
| `PATRON_REQUEST_OUTPUT` | `<SOURCE_DIR>/patron-requests.ndjson` | Joined patron-request output. |

The script requires at least one shard of each configured type.

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

The request includes the `directory.consortium.all` permission header.

## `import-crosslink-patron-requests.sh`

POSTs patron-request NDJSON to the CrossLink broker import endpoint.

Environment variables:

| Variable | Default | Description |
| --- | --- | --- |
| `CROSSLINK_BROKER_HOST` | required | CrossLink broker base URL, for example `http://localhost:8081`. |
| `PATRON_REQUEST_FILE` | `patron-requests.ndjson` | Patron-request NDJSON file to upload. |
| `CONFLICT_POLICY` | `fail` | Import conflict policy passed as the query parameter. |

Example:

```sh
CROSSLINK_BROKER_HOST='http://localhost:8081' \
PATRON_REQUEST_FILE=patron-requests.ndjson \
CONFLICT_POLICY=fail \
./migration/import-crosslink-patron-requests.sh
```

All import scripts use `curl --fail-with-body`, so HTTP failures return a
nonzero exit status while preserving the server response body in the output.
