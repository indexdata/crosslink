GO ?= go
GIT ?= git
GOFMT ?= gofmt "-s"
OS ?= $(shell uname)
COVERAGE=coverage.out

.PHONY: all generate generate-schema lint vulncheck check clean deps-update
XSD2GO = $(GO) run github.com/indexdata/xsd2goxsl@v1.4.0

all:

check:
	$(GO) test -v -cover -coverpkg=./... -coverprofile=$(COVERAGE) ./...

view-coverage: check
	$(GO) tool cover -html=$(COVERAGE)

lint:
	$(GO) run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest run

vulncheck:
	$(GO) run golang.org/x/vuln/cmd/govulncheck@latest ./...

deps-update:
	$(GO) get -u ./...
	$(GO) mod tidy
