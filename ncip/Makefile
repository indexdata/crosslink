GO ?= go
GIT ?= git
GOFMT ?= gofmt "-s"
OS ?= $(shell uname)
XSLT ?= xsltproc
GEN_SCHEMA_IN=xsd/ncip_v2_02.xsd
GEN_SCHEMA_OUT=ncip_gen.go
COVERAGE=coverage.out

.PHONY: all generate generate-schema lint check clean
XSD2GO = $(GO) run github.com/indexdata/xsd2goxsl@9a581ac

all: generate

generate: generate-schema

generate-schema: $(GEN_SCHEMA_OUT)

$(GEN_SCHEMA_OUT): $(GEN_SCHEMA_IN)
	$(XSD2GO) $< $@ \
	'qAttrImport=utils "github.com/indexdata/go-utils/utils"' \
	dateTimeType=utils.XSDDateTime \
	namespaced=yes
	$(GO) fmt $@

clean:
	rm -f $(GEN_SCHEMA_OUT)

check: generate
	$(GO) test -v -cover -coverpkg=./... -coverprofile=$(COVERAGE) ./...

lint: generate
	$(GO) run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest run
