# Directoryish tinkering

Setting up a temporary db (just using public schema for now):
```
docker run -e POSTGRES_PASSWORD=<somepass> -p 54322:5432 -it --rm postgres
PGPASSWORD=<somepass> psql -p 54322 -U postgres -h <local host/ip> -c 'create database directory;'
PGPASSWORD=<somepass> psql -p 54322 -U postgres -h <local host/ip> -d directory -a -f schema.sql
```

Installing sqlc and oapi-codegen (probably good to contrive a way to add these as deps although the [tools.go](https://www.jvt.me/posts/2024/09/30/go-tools-module/) approach is a bit unwieldy it does seem like they have a proposal open with the core team to smooth this out a bit): 
```
go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest
go install github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@latest
```

Generating structs/interfaces for db and api:
```
sqlc generate
oapi-codegen --config=oapi-codegen.yaml api.yaml
```

Compiling:
```
go build
```

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