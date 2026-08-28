GO ?= go
DEPS_UPDATE_CONSTRAINTS := github.com/dprotaso/go-yit@v0.0.0-20250513224043-18a80f8f6df4
GIT ?= git
GOFMT ?= gofmt "-s"
OS ?= $(shell uname)
DOCKER ?= docker
PACKAGES ?= $(shell $(GO) list ./...)
GOFILES := $(shell find . -name "*.go")
MODULE=illmock
COVERAGE=coverage.out
PROGRAMS := $(shell cd cmd; ls)
OAPI_CFG=dirmock/api/oapi-codegen.yaml
OAPI_SPEC=../directory/api.yaml
OAPI_GEN=dirmock/api/directory.gen.go
OAPI_CODEGEN ?= $(GO) tool oapi-codegen

.PHONY: all check clean clean-build-caches coverage docker fmt fmt-check generate run doc view-coverage deps-update lint vulncheck

all: $(PROGRAMS)

docker: generate
	cd .. && $(DOCKER) build -t indexdata/$(MODULE):latest -f ./$(MODULE)/Dockerfile .

generate: $(OAPI_GEN)

$(OAPI_GEN): $(OAPI_CFG) $(OAPI_SPEC)
	$(OAPI_CODEGEN) --config=$(OAPI_CFG) $(OAPI_SPEC)

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

vulncheck: generate
	$(GO) run golang.org/x/vuln/cmd/govulncheck@latest ./...

doc: README.html

README.html: README.md
	pandoc $< -o $@

deps-update:
	$(GO) get -t -u ./... $(DEPS_UPDATE_CONSTRAINTS)
	$(GO) mod tidy

clean:
	rm -f $(PROGRAMS)
	rm -f $(COVERAGE)
	rm -f README.html
	rm -f $(OAPI_GEN)

clean-build-caches:
	$(GO) clean -cache
	$(DOCKER) builder prune --all --force
