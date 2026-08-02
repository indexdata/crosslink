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
