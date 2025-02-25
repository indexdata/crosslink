GO ?= go
GIT ?= git
GOFMT ?= gofmt "-s"
OS ?= $(shell uname)
.PHONY: all generate lint check clean

all: generate

generate: 

check: generate

clenan:

lint: generate
	$(GO) run github.com/golangci/golangci-lint/cmd/golangci-lint@latest run
