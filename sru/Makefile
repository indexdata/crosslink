GO ?= go
GIT ?= git
GOFMT ?= gofmt "-s"
OS ?= $(shell uname)
XSLT ?= xsltproc
GEN_FILES = sruResponse.go diagnostic.go scan.go searchResultAnalysis.go sruResponse.go xcql.go facetedResults.go
.PHONY: all generate generate-schema lint check clean

all: generate

generate: generate-schema 

generate-schema: $(GEN_FILES)

%.go : xsd/%.xsd
	$(GO) run github.com/indexdata/xsd2goxsl@v1.1.0 $< $@ \
		'qAttrImport=utils "github.com/indexdata/go-utils/utils"' \
		package=sru \
		json=yes
	sed -i -E 's/(VersionDefinition[0-9])\./\1_/g' $@
	$(GO) fmt $@

clean:
	rm -f $(GEN_FILES)

check: generate

lint: generate
	$(GO) run github.com/golangci/golangci-lint/cmd/golangci-lint@latest run
