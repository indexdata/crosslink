GO ?= go
GOFMT ?= gofmt "-s"
DOCKER ?= docker
GOFILES := $(shell find . -name "*.go")
BINARY=directory
MAIN_PACKAGE=cmd/directory
COVERAGE=coverage.out
SQLC ?= $(GO) tool sqlc
OAPI_CODEGEN ?= $(GO) tool oapi-codegen
SQL_GEN_IN := sqlc.yaml query.sql $(wildcard migrations/*.sql) go.mod go.sum
SQL_GEN_OUT := db/db.go db/models.go db/query.sql.go
OAPI_GEN_IN := api.yaml oapi-codegen.yaml go.mod go.sum
OAPI_GEN_OUT := api/directory.gen.go

.PHONY: all docker generate generate-sqlc generate-api check checkinclgen run fmt fmt-check lint vulncheck clean deps-update tools-update

all: $(BINARY)

docker:
	cd .. && $(DOCKER) build -f ./directory/Dockerfile .

generate: $(SQL_GEN_OUT) $(OAPI_GEN_OUT)

generate-sqlc: $(SQL_GEN_OUT)

$(SQL_GEN_OUT) &: $(SQL_GEN_IN)
	$(SQLC) generate

generate-api: $(OAPI_GEN_OUT)

$(OAPI_GEN_OUT): $(OAPI_GEN_IN)
	$(OAPI_CODEGEN) --config=oapi-codegen.yaml api.yaml

$(BINARY): $(SQL_GEN_OUT) $(OAPI_GEN_OUT) $(GOFILES)
	$(GO) build -v -o $(BINARY) ./$(MAIN_PACKAGE)

checkinclgen: generate
	$(GO) test -v -cover -coverpkg=./... -coverprofile=$(COVERAGE) ./...

check: generate
	$(GO) test -v -coverpkg=./... -coverprofile=$(COVERAGE).tmp ./...
	grep -v "\.gen\.go" $(COVERAGE).tmp | grep -v "/db/" | grep -v "/test/" > $(COVERAGE)
	$(GO) tool cover -func $(COVERAGE)

run: generate
	$(GO) run -buildvcs=true ./$(MAIN_PACKAGE)

fmt:
	$(GOFMT) -w $(GOFILES)

fmt-check:
	$(GOFMT) -d $(GOFILES)

lint: generate
	$(GO) run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest run

vulncheck: generate
	$(GO) run golang.org/x/vuln/cmd/govulncheck@latest ./...

deps-update:
	$(GO) get -u ./...
	$(GO) mod tidy

tools-update:
	$(GO) get -tool github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@latest
	$(GO) get -tool github.com/sqlc-dev/sqlc/cmd/sqlc@latest
	$(GO) mod tidy

clean:
	rm -f $(BINARY) $(COVERAGE) $(COVERAGE).tmp \
		$(OAPI_GEN_OUT) \
		$(SQL_GEN_OUT)
