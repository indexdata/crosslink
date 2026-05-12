GO ?= go
GIT ?= git
GOFMT ?= gofmt "-s"
OS ?= $(shell uname)
XSLT ?= xsltproc
GEN_SCHEMA_IN=xsd/ncip_v2_02.xsd
GEN_SCHEMA_OUT=ncip_gen.go
COVERAGE=coverage.out

.PHONY: all generate generate-schema lint vulncheck check clean deps-update tools-update
XSD2GO = $(GO) tool xsd2goxsl

all: generate

generate: generate-schema

generate-schema: $(GEN_SCHEMA_OUT)

$(GEN_SCHEMA_OUT): $(GEN_SCHEMA_IN)
	$(XSD2GO) $< $@ \
	'qAttrImport=utils "github.com/indexdata/go-utils/utils"' \
	dateTimeType=utils.XSDDateTime \
	json=yes \
	namespaced=yes
	$(GO) fmt $@

clean:
	rm -f $(GEN_SCHEMA_OUT)

check: generate
	$(GO) test -v -cover -coverpkg=./... -coverprofile=$(COVERAGE) ./...

lint: generate
	$(GO) run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest run

vulncheck: generate
	$(GO) run golang.org/x/vuln/cmd/govulncheck@latest ./...

deps-update:
	$(GO) get -u ./...
	$(GO) mod tidy

tools-update:
	$(GO) get -tool github.com/indexdata/xsd2goxsl@latest
	$(GO) mod tidy
