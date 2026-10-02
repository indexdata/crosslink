GO ?= go
GOFMT ?= gofmt "-s"
DOCKER ?= docker
GOFILES := $(shell find . -name "*.go")
BINARY=supply
DESCRIPTOR=ModuleDescriptor.json
DESCRIPTOR_TEMPLATE=descriptors/ModuleDescriptor-template.json
MAIN_PACKAGE=.
COVERAGE=coverage.out
# Local descriptor version; publishing supplies the chart version explicitly.
VERSION ?= 99.99.99
IMAGE ?= ghcr.io/indexdata/crosslink-supply:latest

.PHONY: all generate check check-coverage run docker fmt fmt-check lint vulncheck clean clean-build-caches view-coverage deps-update $(DESCRIPTOR)

all: $(BINARY) $(DESCRIPTOR)

generate: $(DESCRIPTOR)

$(BINARY): $(GOFILES) go.mod go.sum
	$(GO) build -v -o $(BINARY) ./$(MAIN_PACKAGE)

# Always regenerate so a different VERSION cannot reuse a stale descriptor.
$(DESCRIPTOR): $(DESCRIPTOR_TEMPLATE)
	sed "s/@version@/$(VERSION)/g" $< > $@

check: generate
	$(GO) test -v -cover -coverpkg=./... -coverprofile=$(COVERAGE) ./...

check-coverage: check
	$(GO) run github.com/vladopajic/go-test-coverage/v2@latest --config=./.testcoverage.yaml

run: $(BINARY)
	$(GO) run -buildvcs=true ./$(MAIN_PACKAGE)

docker: generate
	cd .. && $(DOCKER) build -t $(IMAGE) -f ./supply/Dockerfile .

fmt:
	$(GOFMT) -w $(GOFILES)

fmt-check:
	$(GOFMT) -d $(GOFILES)

lint: generate
	$(GO) run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest run

vulncheck: generate
	$(GO) run golang.org/x/vuln/cmd/govulncheck@latest ./...

view-coverage: check
	$(GO) tool cover -html=$(COVERAGE)

deps-update:
	$(GO) get -t -u ./...
	$(GO) mod tidy

clean:
	rm -f $(BINARY) $(DESCRIPTOR) $(COVERAGE) commit.txt

clean-build-caches:
	$(GO) clean -cache
	$(DOCKER) builder prune --all --force
