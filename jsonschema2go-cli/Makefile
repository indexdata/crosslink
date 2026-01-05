GO ?= go
GIT ?= git
GOFMT ?= gofmt "-s"
GOFILES := $(shell find . -name "*.go")
BINARY=jsonschema2go-cli

.PHONY: all generate lint check clean

all: $(BINARY)

generate:

$(BINARY):  $(GOFILES)
	$(GO) build -o $(BINARY)

clean:
	rm -f $(BINARY)

check: generate
	$(GO) test -v -cover -coverpkg=./... -coverprofile=$(COVERAGE) ./...

lint: generate
	$(GO) run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest run
