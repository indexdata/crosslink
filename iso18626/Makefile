GO ?= go
GIT ?= git
GOFMT ?= gofmt "-s"
OS ?= $(shell uname)
XSLT ?= xsltproc
GEN_SCHEMA_IN=xsd/ISO-18626-v1_2.xsd
GEN_SCHEMA_OUT=ill_gen.go

.PHONY: all generate generate-schema lint check clean

all: generate

generate: generate-schema 

generate-schema: $(GEN_SCHEMA_OUT)

$(GEN_SCHEMA_OUT): $(GEN_SCHEMA_IN)
	$(GO) run github.com/indexdata/xsd2goxsl@v1.1.0 $(GEN_SCHEMA_IN) $(GEN_SCHEMA_OUT) \
	'qAttrImport=utils "github.com/indexdata/go-utils/utils"' \
	qAttrType=utils.PrefixAttr \
	dateTimeType=utils.XSDDateTime \
	decimalType=utils.XSDDecimal \
	json=yes
	$(GO) fmt $(GEN_SCHEMA_OUT)

clean:
	rm -f $(GEN_SCHEMA_OUT)

check: generate

lint: generate
	$(GO) run github.com/golangci/golangci-lint/cmd/golangci-lint@latest run
