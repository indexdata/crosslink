GO ?= go
GIT ?= git
GOFMT ?= gofmt "-s"
OS ?= $(shell uname)
XSLT ?= xsltproc
GEN_FILES = sruSimpleResponse.go diagnostic.go
.PHONY: all generate generate-schema lint check clean
XSD2GO = $(GO) run github.com/indexdata/xsd2goxsl@b3de6c4

all: generate

generate: generate-schema 

generate-schema: $(GEN_FILES)

%.go : xsd/%.xsd
	$(XSD2GO) $< $@ \
		namespaced=yes \
		package=sru \
		json=yes
	$(GO) fmt $@

clean:
	rm -f $(GEN_FILES)

check: generate

lint: generate
	$(GO) run github.com/golangci/golangci-lint/cmd/golangci-lint@latest run
