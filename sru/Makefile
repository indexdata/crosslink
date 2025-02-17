GO ?= go
GIT ?= git
GOFMT ?= gofmt "-s"
OS ?= $(shell uname)
XSLT ?= xsltproc
GEN_FILES = sruSimpleResponse.go diagnostic.go
.PHONY: all generate generate-schema lint check clean
#XSD2GO = $(GO) run github.com/indexdata/xsd2goxsl@v1.1.0
XSD2GO = ../../xsd2goxsl/xsd2goxsl

all: generate

generate: generate-schema 

generate-schema: $(GEN_FILES)

%.go : xsd/%.xsd
	$(XSD2GO) $< $@ \
		'qAttrImport=utils "github.com/indexdata/go-utils/utils"' \
		package=sru \
		json=yes
#	sed -i -E 's/(VersionDefinition[0-9])\./\1_/g' $@  // until we fix the xsd2goxsl
	$(GO) fmt $@

clean:
	rm -f $(GEN_FILES)

check: generate

lint: generate
	$(GO) run github.com/golangci/golangci-lint/cmd/golangci-lint@latest run
