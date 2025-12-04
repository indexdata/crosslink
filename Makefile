GO ?= go
GIT ?= git
GOFMT ?= gofmt "-s"
OS ?= $(shell uname)
DOCKER ?= docker
GOFILES := $(shell find . -name "*.go")
GOTOOLS=tools.go
BINARY=directory
MAIN_PACKAGE=cmd/directory
COVERAGE=coverage.out

all: $(BINARY)

docker:
	cd .. && $(DOCKER) build -f ./$(MODULE)/Dockerfile .

generate:
	$(GO) generate

verify-gen:
	$(GO) generate
	$(GIT) diff --exit-code --quiet -- ':/*.gen.go' 'db/' || (echo "Error: Generated code is out of date. Run 'make generate' and commit the changes." && exit 1)

$(COMMIT_ID):
	$(GIT) rev-parse --short HEAD | tr -d '\n' > $(COMMIT_ID)

$(BINARY):  $(COMMIT_ID) $(GEN_SCHEMA_OUT) $(SQL_GEN_OUT) $(GOFILES)
	$(GO) build -v -o $(BINARY) ./$(MAIN_PACKAGE)

checkinclgen:
	$(GO) test -v -cover -coverpkg=./... -coverprofile=$(COVERAGE) ./...

check:
	$(GO) test -v -coverpkg=./... -coverprofile=$(COVERAGE).tmp ./...
	grep -v "\.gen\.go" $(COVERAGE).tmp | grep -v "/db/" | grep -v "/test/" > $(COVERAGE)
	$(GO) tool cover -func $(COVERAGE)

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
