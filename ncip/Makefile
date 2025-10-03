GO ?= go
GIT ?= git
GOFMT ?= gofmt "-s"
OS ?= $(shell uname)
XSLT ?= xsltproc
GEN_SCHEMA_IN=xsd/ncip_v2_02.xsd
GEN_SCHEMA_OUT=ncip_gen.go
COVERAGE=coverage.out

.PHONY: all generate generate-schema lint check clean

all: generate

generate: generate-schema

generate-schema: $(GEN_SCHEMA_OUT)

$(GEN_SCHEMA_OUT): $(GEN_SCHEMA_IN)
	$(GO) run github.com/indexdata/xsd2goxsl@v1.1.0 $(GEN_SCHEMA_IN) $(GEN_SCHEMA_OUT) \
	json=yes
	$(GO) fmt $(GEN_SCHEMA_OUT)

clean:
	rm -f $(GEN_SCHEMA_OUT)

check: generate
	$(GO) test -v -cover -coverpkg=./... -coverprofile=$(COVERAGE) ./...

lint: generate
	$(GO) run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest run
