GO ?= go
GIT ?= git
GOFMT ?= gofmt "-s"
OS ?= $(shell uname)
XSLT ?= xsltproc
GEN_FILES = MARC21slim_gen.go opacxml_gen.go
.PHONY: all generate generate-schema lint vulncheck check clean deps-update
XSD2GO = $(GO) run github.com/indexdata/xsd2goxsl@912e22d86f2c44d700dbabf3411442f81553f39b

all: generate

generate: generate-schema

generate-schema: $(GEN_FILES)

MARC21slim_gen.go : xsd/MARC21slim.xsd
	$(XSD2GO) $< $@ \
		namespaced=yes \
		package=marcxml \
		json=yes
	$(GO) fmt $@

opacxml_gen.go : xsd/opacxml.xsd
	$(XSD2GO) $< $@ \
		namespaced=no \
		package=marcxml \
		json=yes
	$(GO) fmt $@

clean:
	rm -f $(GEN_FILES)

check: generate

lint: generate
	$(GO) run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest run

vulncheck: generate
	$(GO) run golang.org/x/vuln/cmd/govulncheck@latest ./...

deps-update:
	$(GO) get -u ./...
	$(GO) mod tidy
