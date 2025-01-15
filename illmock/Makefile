GO ?= go
GIT ?= git
GOFMT ?= gofmt "-s"
OS ?= $(shell uname)
DOCKER ?= docker
PACKAGES ?= $(shell $(GO) list ./...)
GOFILES := $(shell find . -name "*.go")
BINARY=illmock
MODULE=illmock
COMMIT_ID=commit.txt
COVERAGE=coverage.out

.PHONY: all docker generate check run fmt fmt-check vet clean

all: $(BINARY)

# TODO: No Dockerfile
docker: generate
	cd .. && $(DOCKER) build -f ./$(MODULE)/Dockerfile .

generate: generate-commit-id

generate-commit-id: $(COMMIT_ID)

$(COMMIT_ID):
	$(GIT) rev-parse --short HEAD | tr -d '\n' > $(COMMIT_ID)

$(BINARY):  $(COMMIT_ID) $(GOFILES)
	$(GO) build -v -o $(BINARY)

check: generate
	$(GO) test -v -cover -coverpkg=./... -coverprofile=$(COVERAGE) ./...

run: $(BINARY)
	$(GO) run -buildvcs=true

fmt:
	$(GOFMT) -w $(GOFILES)

fmt-check:
	$(GOFMT) -d $(GOFILES)

lint: generate
	$(GO) run github.com/golangci/golangci-lint/cmd/golangci-lint@latest run

clean:
	rm -f $(BINARY)
	rm -f $(COVERAGE)
	rm -f $(COMMIT_ID)
