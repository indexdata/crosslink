GO ?= go
GIT ?= git
GOFMT ?= gofmt "-s"
OS ?= $(shell uname)

OAPI_CFG = directory_config.yaml
OAPI_SPEC = directory_api.yaml
OAPI_GEN = directory_gen.go
OAPI_CODEGEN ?= $(GO) tool oapi-codegen
COVERAGE=coverage.out

.PHONY: all generate lint vulncheck check clean deps-update deps-update-tools

all: generate

generate: $(OAPI_GEN)

$(OAPI_GEN): $(OAPI_CFG) $(OAPI_SPEC)
	$(OAPI_CODEGEN) -config ./$(OAPI_CFG) ./$(OAPI_SPEC)

clean:
	rm -f $(OAPI_GEN) $(COVERAGE)

check: generate
	$(GO) test -v -cover -coverpkg=./... -coverprofile=$(COVERAGE) ./...

lint: generate
	$(GO) run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest run

vulncheck: generate
	$(GO) run golang.org/x/vuln/cmd/govulncheck@latest ./...

deps-update:
	$(GO) get -u ./...
	$(GO) mod tidy

deps-update-tools:
	$(GO) get -tool github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@latest
	$(GO) mod tidy
