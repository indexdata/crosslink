GO ?= go
GIT ?= git
GOFMT ?= gofmt "-s"
OS ?= $(shell uname)
DOCKER ?= docker
PACKAGES ?= $(shell $(GO) list ./...)
GOFILES := $(shell find . -name "*.go")
MODULE=illmock
COMMIT_ID=commit.txt
COVERAGE=coverage.out
PROGRAMS := $(shell cd cmd; ls)

# OpenAPI
OAPI_DIR=directory
OAPI_CFG = $(OAPI_DIR)/directory_config.yaml
OAPI_SPEC = $(OAPI_DIR)/directory_api.yaml
OAPI_GEN = $(OAPI_DIR)/directory_gen.go

.PHONY: all check clean coverage docker fmt fmt-check generate run doc view-coverage

all: $(PROGRAMS)

docker: generate
	cd .. && $(DOCKER) build -t indexdata/$(MODULE):latest -f ./$(MODULE)/Dockerfile .

generate: $(COMMIT_ID) $(OAPI_GEN)

$(OAPI_GEN): $(OAPI_CFG) $(OAPI_SPEC)
	$(GO) run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen -config ./$(OAPI_CFG) ./$(OAPI_SPEC)

$(COMMIT_ID):
	$(GIT) rev-parse --short HEAD | tr -d '\n' > $(COMMIT_ID)

$(PROGRAMS): $(GOFILES) $(COMMIT_ID) $(OAPI_GEN)
	$(GO) build -v -o $@ ./cmd/$@

check: generate
	$(GO) test -v -cover -coverpkg=./... -coverprofile=$(COVERAGE) ./...

view-coverage: check
	$(GO) tool cover -html=$(COVERAGE)

check-coverage: check
	$(GO) run github.com/vladopajic/go-test-coverage/v2@latest --config=./.testcoverage.yaml

fmt:
	$(GOFMT) -w $(GOFILES)

fmt-check:
	$(GOFMT) -d $(GOFILES)

lint: generate
	$(GO) run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest run

doc: README.html

README.html: README.md
	pandoc $< -o $@

clean:
	$(GO) clean -testcache
	$(GO) clean -cache
	rm -f $(PROGRAMS)
	rm -f $(COVERAGE)
	rm -f $(COMMIT_ID)
	rm -f $(OAPI_GEN)
	rm -f README.html
