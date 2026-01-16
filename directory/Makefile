GO ?= go
GIT ?= git
GOFMT ?= gofmt "-s"
OS ?= $(shell uname)

OAPI_CFG = directory_config.yaml
OAPI_SPEC = directory_api.yaml
OAPI_GEN = directory_gen.go
COVERAGE=coverage.out

.PHONY: all generate lint check clean

all: generate

generate: $(OAPI_GEN)

$(OAPI_GEN): $(OAPI_CFG) $(OAPI_SPEC)
	$(GO) run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen -config ./$(OAPI_CFG) ./$(OAPI_SPEC)

clean:
	rm -f $(OAPI_GEN) $(COVERAGE)

check: generate
	$(GO) test -v -cover -coverpkg=./... -coverprofile=$(COVERAGE) ./...

lint: generate
	$(GO) run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest run
