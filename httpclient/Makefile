GO ?= go
GIT ?= git
GOFMT ?= gofmt "-s"
OS ?= $(shell uname)
COVERAGE=coverage.out
.PHONY: all generate lint check clean

all:

check:
	$(GO) test -v -cover -coverpkg=./... -coverprofile=$(COVERAGE) ./...

view-coverage: check
	$(GO) tool cover -html=$(COVERAGE)

lint: generate
	$(GO) run github.com/golangci/golangci-lint/cmd/golangci-lint@latest run

check-coverage: check
	$(GO) run github.com/vladopajic/go-test-coverage/v2@latest --config=./.testcoverage.yaml

clean:
	rm -f $(COVERAGE)
