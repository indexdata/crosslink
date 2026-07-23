GO ?= go
GOFMT ?= gofmt "-s"
DOCKER ?= docker
GOFILES := $(shell find . -name "*.go")
BINARY=directory
MAIN_PACKAGE=cmd/directory
COVERAGE=coverage.out
SQLC ?= $(GO) tool sqlc
OAPI_CODEGEN ?= $(GO) tool oapi-codegen

.PHONY: all docker generate generate-sqlc generate-api check checkinclgen run fmt fmt-check lint vulncheck clean deps-update tools-update

all: $(BINARY)

docker:
	cd .. && $(DOCKER) build -f ./directory/Dockerfile .

generate: generate-sqlc generate-api

generate-sqlc:
	$(SQLC) generate

generate-api:
	$(OAPI_CODEGEN) --config=oapi-codegen.yaml api.yaml

$(BINARY): generate $(GOFILES)
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
		api/directory.gen.go \
		db/db.go db/models.go db/query.sql.go
