GO ?= go
GIT ?= git
GOFMT ?= gofmt "-s"
OS ?= $(shell uname)
DOCKER ?= docker
GOFILES := $(shell find . -name "*.go")
GOTOOLS=tools.go
BINARY=directoryish
MAIN_PACKAGE=cmd/directory
COVERAGE=coverage.out

all: $(BINARY)

docker:
	cd .. && $(DOCKER) build -f ./$(MODULE)/Dockerfile .

generate:
	$(GO) generate

$(COMMIT_ID):
	$(GIT) rev-parse --short HEAD | tr -d '\n' > $(COMMIT_ID)

$(BINARY):  $(COMMIT_ID) $(GEN_SCHEMA_OUT) $(SQL_GEN_OUT) $(GOFILES)
	$(GO) build -v -o $(BINARY) ./$(MAIN_PACKAGE)

check:
	$(GO) test -v -cover -coverpkg=./... -coverprofile=$(COVERAGE) ./...

run: $(BINARY)
	$(GO) run -buildvcs=true ./$(MAIN_PACKAGE)

fmt:
	$(GOFMT) -w $(GOFILES)

fmt-check:
	$(GOFMT) -d $(GOFILES)

lint:
	$(GO) run github.com/golangci/golangci-lint/cmd/golangci-lint@latest run

clean:
	rm -f $(BINARY)
	rm -f $(COVERAGE)
	rm -f $(COMMIT_ID)
