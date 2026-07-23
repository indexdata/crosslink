# Directory tinkering

Setting up a temporary db (just using public schema for now):
```
docker run -e POSTGRES_PASSWORD=<somepass> -p 54322:5432 -it --rm postgres
PGPASSWORD=<somepass> psql -p 54322 -U postgres -h <local host/ip> -c 'create database directory;'
PGPASSWORD=<somepass> psql -p 54322 -U postgres -h <local host/ip> -d directory -a -f schema.sql
```

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

# Some examples of repos using sqlc / some sort of api gen

## Contrived
- https://github.com/SeaRoll/api-sqlc-goose/tree/main
- https://github.com/danicc097/openapi-go-gin-postgres-sqlc
- https://github.com/kwryoh/oapi-sample
- https://github.com/aliml92/realworld-gin-sqlc/tree/master

## Real
- https://github.com/leg100/otf
- https://github.com/helpwave/services/tree/main/services/tasks-svc

# Environment variables
- SYMBOL_AUTHORITY: The authority that is paired with the incoming institution/tenant to form a full symbol
