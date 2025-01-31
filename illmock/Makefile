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

# TODO remove this when iso18626 package is move to top-level
BROKER_DIR = ../broker
BROKER_TARGET = generate

.PHONY: all check clean coverage docker fmt fmt-check generate run doc $(BROKER_DIR)/$(BROKER_TARGET)

all: $(PROGRAMS)

docker: generate
	cd .. && $(DOCKER) build -t indexdata/$(MODULE):latest -f ./$(MODULE)/Dockerfile .

generate: generate-commit-id $(BROKER_DIR)/$(BROKER_TARGET)

$(BROKER_DIR)/$(BROKER_TARGET):
	$(MAKE) -C $(BROKER_DIR) $(BROKER_TARGET)

generate-commit-id: $(COMMIT_ID)

$(COMMIT_ID):
	$(GIT) rev-parse --short HEAD | tr -d '\n' > $(COMMIT_ID)

$(PROGRAMS): generate $(GOFILES)
	$(GO) build -v -o $@ ./cmd/$@

check: generate
	$(GO) test -v -cover -coverpkg=./... -coverprofile=$(COVERAGE) ./...

coverage: check
	$(GO) tool cover -html=$(COVERAGE)

fmt:
	$(GOFMT) -w $(GOFILES)

fmt-check:
	$(GOFMT) -d $(GOFILES)

lint: generate
	$(GO) run github.com/golangci/golangci-lint/cmd/golangci-lint@latest run

doc: README.html

README.html: README.md
	pandoc $< -o $@

clean:
	rm -f $(PROGRAMS)
	rm -f $(COVERAGE)
	rm -f $(COMMIT_ID)
	rm -f README.html
