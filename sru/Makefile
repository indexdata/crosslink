GO ?= go
GIT ?= git
GOFMT ?= gofmt "-s"
OS ?= $(shell uname)
XSLT ?= xsltproc
GEN_SCHEMA_IN=xsd/sruResponse.xsd
GEN_SCHEMA_OUT=schema.go

.PHONY: all generate generate-schema lint check clean

all: generate

generate: generate-schema 

generate-schema: $(GEN_SCHEMA_OUT)

$(GEN_SCHEMA_OUT): $(GEN_SCHEMA_IN)
	$(GO) run github.com/indexdata/xsd2goxsl@v1.1.0 $(GEN_SCHEMA_IN) $(GEN_SCHEMA_OUT) \
	'qAttrImport=utils "github.com/indexdata/go-utils/utils"' \
	package=sru
	json=yes
	sed -i -E 's/(VersionDefinition[0-9])\./\1_/g' $(GEN_SCHEMA_OUT)
	$(GO) fmt $(GEN_SCHEMA_OUT)

clean:
	rm -f $(GEN_SCHEMA_OUT)

check: generate

lint: generate
	$(GO) run github.com/golangci/golangci-lint/cmd/golangci-lint@latest run
