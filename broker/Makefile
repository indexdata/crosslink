GO ?= go
GIT ?= git
GOFMT ?= gofmt "-s"
OS ?= $(shell uname)
DOCKER ?= docker
XSLT ?= xsltproc
PACKAGES ?= $(shell $(GO) list ./...)
GOFILES := $(shell find . -name "*.go")
GOTOOLS=tools.go
BINARY=broker
MAIN_PACKAGE=cmd/broker
MODULE=broker
GEN_SCHEMA_IN=xsd/ISO-18626-v1_2.xsd
GEN_SCHEMA_OUT=iso18626/schema.go
COMMIT_ID=cmd/broker/commit.txt
COVERAGE=coverage.out

# Directories
SQLC ?= sqlc
SQLC_CONFIG = db/sqlc.yaml
SQL_GEN_OUT_DIR = db/generated
SQL_GEN_OUT = $(SQL_GEN_OUT_DIR)/*.go
SQL_GEN_IN = db/*.sql

.PHONY: all docker generate generate-schema generate-commit-id check run fmt fmt-check vet clean

all: $(BINARY)

docker: generate
	cd .. && $(DOCKER) build -f ./$(MODULE)/Dockerfile .

generate: generate-schema generate-commit-id generate-sqlc

generate-schema: $(GEN_SCHEMA_OUT)

generate-commit-id: $(COMMIT_ID)

generate-sqlc: $(SQL_GEN_OUT)

$(SQL_GEN_OUT): $(SQL_GEN_IN) $(SQLC_CONFIG)
	$(SQLC) generate -f $(SQLC_CONFIG)

$(COMMIT_ID):
	$(GIT) rev-parse --short HEAD | tr -d '\n' > $(COMMIT_ID)

$(GEN_SCHEMA_OUT): $(GEN_SCHEMA_IN)
	$(GO) run github.com/indexdata/xsd2goxsl@v1.1.0 $(GEN_SCHEMA_IN) $(GEN_SCHEMA_OUT) \
	'qAttrImport=utils "github.com/indexdata/go-utils/utils"' \
	qAttrType=utils.PrefixAttr \
	dateTimeType=utils.XSDDateTime \
	decimalType=utils.XSDDecimal \
	json=yes
	$(GO) fmt $(GEN_SCHEMA_OUT)

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

lint:
	$(GO) run github.com/golangci/golangci-lint/cmd/golangci-lint@latest run

clean:
	rm -f $(BINARY)
	rm -f $(GEN_SCHEMA_OUT)
	rm -f $(COVERAGE)
	rm -f $(COMMIT_ID)
	rm -fr $(SQL_GEN_OUT_DIR)
