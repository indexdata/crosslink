GO ?= go
GIT ?= git
GOFMT ?= gofmt "-s"
OS ?= $(shell uname)
DOCKER ?= docker
ifeq (Linux, $(OS))
export HOST_DOCKER_INTERNAL=172.17.0.1
endif
PACKAGES ?= $(shell $(GO) list ./...)
GOFILES := $(shell find . -name "*.go")
GOTOOLS=tools.go
BINARY=broker
MAIN_PACKAGE=cmd/broker
MODULE=broker
COMMIT_ID=cmd/broker/commit.txt
COVERAGE=coverage.out

# Directories
SQLC ?= sqlc
SQLC_CONFIG = sqlc/sqlc.yaml
SQL_GEN_OUT_ILL_DB = ill_db/*-gen.go
SQL_GEN_OUT_EVENT = events/*-gen.go
SQL_GEN_OUT = $(SQL_GEN_OUT_ILL_DB) $(SQL_GEN_OUT_EVENT)
SQL_GEN_IN = sqlc/*.sql

.PHONY: all docker generate generate-sqlc generate-commit-id check run fmt fmt-check vet clean view-coverage

all: $(BINARY)

docker: generate
	cd .. && $(DOCKER) build -t indexdata/$(MODULE):latest -f ./$(MODULE)/Dockerfile .

generate: generate-commit-id generate-sqlc

generate-commit-id: $(COMMIT_ID)

generate-sqlc: $(SQL_GEN_OUT)

$(SQL_GEN_OUT): $(SQL_GEN_IN) $(SQLC_CONFIG)
	$(SQLC) generate -f $(SQLC_CONFIG)

$(COMMIT_ID):
	$(GIT) rev-parse --short HEAD | tr -d '\n' > $(COMMIT_ID)

$(BINARY):  $(COMMIT_ID) $(GEN_SCHEMA_OUT) $(SQL_GEN_OUT) $(GOFILES)
	$(GO) build -v -o $(BINARY) ./$(MAIN_PACKAGE)

check: generate
	$(GO) test -v -cover -coverpkg=./... -coverprofile=$(COVERAGE) ./...

run: $(BINARY)
	$(GO) run -buildvcs=true ./$(MAIN_PACKAGE)

fmt:
	$(GOFMT) -w $(GOFILES)

fmt-check:
	$(GOFMT) -d $(GOFILES)

lint: generate
	$(GO) run github.com/golangci/golangci-lint/cmd/golangci-lint@latest run

view-coverage: check
	$(GO) tool cover -html=$(COVERAGE)

check-coverage: check
	$(GO) run github.com/vladopajic/go-test-coverage/v2@latest --config=./.testcoverage.yaml

clean:
	rm -f $(BINARY)
	rm -f $(COVERAGE)
	rm -f $(COMMIT_ID)
	rm -f $(SQL_GEN_OUT)
