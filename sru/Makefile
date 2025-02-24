GO ?= go
GIT ?= git
GOFMT ?= gofmt "-s"
OS ?= $(shell uname)
XSLT ?= xsltproc
GEN_FILES = sru_gen.go \
	diag/diag_gen.go \
	facet/facet_gen.go \
	sra/sra_gen.go \
	xcql/xcql_gen.go \
	scan/scan_gen.go

.PHONY: all generate generate-schema lint check clean
XSD2GO = $(GO) run github.com/indexdata/xsd2goxsl@9a581ac

define nsImports
namespaced=yes \
"namespaceImports=http://docs.oasis-open.org/ns/search-ws/diagnostic=github.com/indexdata/crosslink/sru/diag,\
http://docs.oasis-open.org/ns/search-ws/facetedResults=github.com/indexdata/crosslink/sru/facet,\
http://docs.oasis-open.org/ns/search-ws/searchResultAnalysis=github.com/indexdata/crosslink/sru/sra,\
http://docs.oasis-open.org/ns/search-ws/xcql=github.com/indexdata/crosslink/sru/xcql,\
http://docs.oasis-open.org/ns/search-ws/scan=github.com/indexdata/crosslink/sru/scan"
endef

all: generate

generate: generate-schema 

generate-schema: $(GEN_FILES)

diag/diag_gen.go: xsd/diagnostic.xsd
	mkdir -p $(dir $@)
	$(XSD2GO) $< $@ $(nsImports) package=diag
	$(GO) fmt $@

facet/facet_gen.go: xsd/facetedResults.xsd
	mkdir -p $(dir $@)
	$(XSD2GO) $< $@ $(nsImports) package=facet
	$(GO) fmt $@

sra/sra_gen.go: xsd/searchResultAnalysis.xsd
	mkdir -p $(dir $@)
	$(XSD2GO) $< $@ $(nsImports) package=sra
	$(GO) fmt $@

xcql/xcql_gen.go: xsd/xcql.xsd
	mkdir -p $(dir $@)
	$(XSD2GO) $< $@ $(nsImports) package=xcql
	$(GO) fmt $@

scan/scan_gen.go: xsd/scan.xsd
	mkdir -p $(dir $@)
	$(XSD2GO) $< $@ $(nsImports) package=scan
	$(GO) fmt $@

sru_gen.go: xsd/sruResponse.xsd
	$(XSD2GO) $< $@ $(nsImports) package=sru
	$(GO) fmt $@

clean:
	rm -f $(GEN_FILES)

check: generate

lint: generate
	$(GO) run github.com/golangci/golangci-lint/cmd/golangci-lint@latest run
