# Directory service

## Local database

Start a temporary PostgreSQL server and create the Directory database:

```
docker run --name crosslink-directory-postgres --rm -d \
  -e POSTGRES_PASSWORD=directory -p 54322:5432 postgres
until docker exec crosslink-directory-postgres pg_isready -U postgres; do sleep 1; done
PGPASSWORD=directory psql -p 54322 -U postgres -h localhost \
  -c 'create database directory;'
```

Run the service with a matching connection string. Database migrations are
applied automatically during startup:

```
DATABASE_URL=postgresql://postgres:directory@localhost:54322/directory make run
```

## Build and test

The SQLC and OpenAPI generator versions are pinned as Go tools in `go.mod`.
Generate the database and API sources with:

```
make generate
```

Generated Go sources are build artifacts and are not stored in the repository.
The standard build, test, lint, and run targets generate them automatically:

```
make all
make check
make lint
make run
```

Run `make generate` before invoking `go build` or `go test` directly.

## One-time data load through Okapi

`scripts/load_directory_data.py` loads an exported Directory dataset, such as
`../illmock/dirmock/directories.json`, into an existing consortium. It creates
each unique tier and network once, remaps the generated identifiers, creates
Institutions and their Branches in dependency order, and then restores tier and
network memberships.

The supplied API endpoint must end in `/directory` or be a server root to which
the script can append that path. Put the Okapi token in the environment so it
is not exposed in the command line:

```
export DIRECTORY_TOKEN='replace-with-an-okapi-token'
python3 scripts/load_directory_data.py \
  --base-url 'https://okapi.example/directory' \
  --tenant 'example' \
  --consortium-id '00000000-0000-0000-0000-000000000000' \
  --fixture '../illmock/dirmock/directories.json' \
  --dry-run
```

The dry run validates the complete source graph, verifies that the target is a
Consortium entry, and checks for conflicting entry names, symbols, tier names,
and network names without creating anything. Remove `--dry-run` to perform the
load:

```
python3 scripts/load_directory_data.py \
  --base-url 'https://okapi.example/directory' \
  --tenant 'example' \
  --consortium-id '00000000-0000-0000-0000-000000000000' \
  --fixture '../illmock/dirmock/directories.json' \
  --allow-existing \
  --delete-entries \
  --piecemeal \
  --replace-localhost 'https://services.example' \
  --result './directory-load-result.json'
```

By default, the preflight fails if a tier or network in the fixture has the
same name as one already owned by the target consortium. With
`--allow-existing`, existing tiers and networks are reused by exact name.
Existing entries are reused when their name or symbol identifies one entry of
the same type in the target consortium, and Branch parents must resolve to the
reused source Institution. Existing memberships are also reused. A conflicting
network priority or an ambiguous name/symbol match still fails during
preflight.

With `--delete-entries`, preflight finds existing entries in the target
consortium that match a fixture entry by exact name or symbol. After all
matches have been validated, the load deletes matching Branches before their
matching Institutions, then creates the fixture entries normally. Preflight
rejects ambiguous matches, symbol matches outside the target consortium, type
mismatches, and a matching Institution that has a child which is not also
matched for deletion. A dry run reports the number as `entriesToDelete` but
does not delete anything. When combined with `--allow-existing`, entries are
replaced while tiers and networks are still reused.

Use `--verify` to perform a read-only check that the fixture has already been
loaded:

```
python3 scripts/load_directory_data.py \
  --base-url 'https://okapi.example/directory' \
  --tenant 'example' \
  --consortium-id '00000000-0000-0000-0000-000000000000' \
  --fixture '../illmock/dirmock/directories.json' \
  --replace-localhost 'https://services.example' \
  --verify
```

Verification resolves entries by exact name or symbol and tiers and networks
by exact name, without requiring source UUIDs to match. It checks the entry
types and parent relationships, all fields supplied by the fixture, tier and
network definitions, memberships, and network priorities. Additional server
fields and memberships are allowed. Localhost URL replacement is applied to
the fixture before comparison. Verification sends only GET requests and cannot
be combined with `--delete-entries`; a successful manifest has status
`verified`.

With `--piecemeal`, each entry is first created without `symbols`, `lmsConfig`,
`endpoints`, `holdingsPolicy`, or `catalogConfig`. Each field present in the
source is then applied in its own PATCH request. This can help isolate gateway
or WAF rules that reject a combined entry payload. Successfully patched fields
are recorded in the result manifest.

With `--replace-localhost URL`, every absolute HTTP or HTTPS URL in the source
whose hostname is exactly `localhost` is rewritten before validation and
upload. The replacement URL supplies the scheme, host, and port; the original
path, query string, and fragment are retained. If the replacement URL includes
a path prefix, it is prepended to the original path. Other strings and URLs are
unchanged. The replacement URL and number of rewritten values are recorded in
the result manifest.

Every request includes the token as `X-Okapi-Token` and the supplied tenant as
`X-Okapi-Tenant`. The script does not send `X-Okapi-Permissions`; the token must
grant `directory.consortium.all`.

The result manifest is updated after each successful creation or deletion and
contains the source-to-created UUID mappings. Deleted entries are recorded in
`deletedEntries`. A failed multi-request load is not rolled back automatically;
entry deletion is irreversible through this script. Use the manifest's records
to identify what changed before the failure. The token is never written to the
manifest or printed in an error message. API errors include the attempted URL,
request headers and content, and response headers for troubleshooting. The
`X-Okapi-Token` header, cookies, and other sensitive header values are redacted.

## Some examples of repositories using SQLC or API generation

### Contrived
- https://github.com/SeaRoll/api-sqlc-goose/tree/main
- https://github.com/danicc097/openapi-go-gin-postgres-sqlc
- https://github.com/kwryoh/oapi-sample
- https://github.com/aliml92/realworld-gin-sqlc/tree/master

### Real
- https://github.com/leg100/otf
- https://github.com/helpwave/services/tree/main/services/tasks-svc

## Environment variables

| Name                      | Description                                                                    | Default value                                               |
|---------------------------|--------------------------------------------------------------------------------|-------------------------------------------------------------|
| `HOST`                    | Address on which the HTTP server listens                                       | `localhost`                                                 |
| `HTTP_PORT`               | Port on which the HTTP server listens                                          | `8086`                                                      |
| `DATABASE_URL`            | PostgreSQL connection string used by the service and database migrations       | `postgresql://postgres:directory@localhost:54322/directory` |
| `TENANT_SYMBOL_AUTHORITY` | Authority paired with an incoming institution/tenant to form a complete symbol | `TEST`                                                      |
| `LOG_LEVEL`               | Log level: `debug`, `info`, `warn`, or `error`                                 | `info`                                                      |
| `LOG_FORMAT`              | Log output format; set to `json` for structured JSON logs                      | `text`                                                      |
