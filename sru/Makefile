GO ?= go
GIT ?= git
GOFMT ?= gofmt "-s"
OS ?= $(shell uname)
XSLT ?= xsltproc
GEN_FILES = sruSimpleResponse.go diagnostic.go
.PHONY: all generate generate-schema lint check clean
XSD2GO = $(GO) run github.com/indexdata/xsd2goxsl@7e016ae

all: generate

generate: generate-schema 

generate-schema: $(GEN_FILES)

%.go : xsd/%.xsd
	$(XSD2GO) $< $@ \
		'qAttrImport=utils "github.com/indexdata/go-utils/utils"' \
		namespaced=yes \
		package=sru \
		json=yes
	$(GO) fmt $@

clean:
	rm -f $(GEN_FILES)

check: generate

lint: generate
	$(GO) run github.com/golangci/golangci-lint/cmd/golangci-lint@latest run
