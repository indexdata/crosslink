GO ?= go
GIT ?= git
GOFMT ?= gofmt "-s"
OS ?= $(shell uname)
XSLT ?= xsltproc
GEN_FILES = sru.go \
	diagnostic/diagnostic.go \
	facetedresults/facetedresults.go \
	searchresultanalysis/searchresultanalysis.go \
	xcql/xcql.go \
	scan/scan.go

.PHONY: all generate generate-schema lint check clean
XSD2GO = $(GO) run github.com/indexdata/xsd2goxsl@9a581ac

define nsImports
namespaceImports=http://docs.oasis-open.org/ns/search-ws/diagnostic=github.com/indexdata/crosslink/sru/diagnostic,\
http://docs.oasis-open.org/ns/search-ws/facetedResults=github.com/indexdata/crosslink/sru/facetedresults,\
http://docs.oasis-open.org/ns/search-ws/searchResultAnalysis=github.com/indexdata/crosslink/sru/searchresultanalysis,\
http://docs.oasis-open.org/ns/search-ws/xcql=github.com/indexdata/crosslink/sru/xcql,\
http://docs.oasis-open.org/ns/search-ws/scan=github.com/indexdata/crosslink/sru/scan
endef

all: generate

generate: generate-schema 

generate-schema: $(GEN_FILES)

diagnostic/diagnostic.go: xsd/diagnostic.xsd
	mkdir -p $(dir $@)
	$(XSD2GO) $< $@ "$(nsImports)" package=$(basename $(notdir $@))

facetedresults/facetedresults.go: xsd/facetedResults.xsd
	mkdir -p $(dir $@)
	$(XSD2GO) $< $@ "$(nsImports)" package=$(basename $(notdir $@))

searchresultanalysis/searchresultanalysis.go: xsd/searchResultAnalysis.xsd
	mkdir -p $(dir $@)
	$(XSD2GO) $< $@ "$(nsImports)" package=$(basename $(notdir $@))

xcql/xcql.go: xsd/xcql.xsd
	mkdir -p $(dir $@)
	$(XSD2GO) $< $@ "$(nsImports)" package=$(basename $(notdir $@))

scan/scan.go: xsd/scan.xsd
	mkdir -p $(dir $@)
	$(XSD2GO) $< $@ "$(nsImports)" package=$(basename $(notdir $@))

sru.go: xsd/sruResponse.xsd
	$(XSD2GO) $< $@ "$(nsImports)" package=sru

clean:
	rm -f $(GEN_FILES)

check: generate

lint: generate
	$(GO) run github.com/golangci/golangci-lint/cmd/golangci-lint@latest run
