GO ?= go
GIT ?= git
GOFMT ?= gofmt "-s"
OS ?= $(shell uname)
DOCKER ?= docker
GOFILES := $(shell find . -name "*.go")
BINARY=mod-dms
DESCRIPTOR=ModuleDescriptor.json
MAIN_PACKAGE=main.go
COMMIT_ID=commit.txt
COVERAGE=coverage.out
VERSION ?= `$(GIT) describe --tags --abbrev=0 | sed 's/^v\([0-9]\)/\1/'`

all: $(BINARY) $(DESCRIPTOR)

docker:
	cd .. && $(DOCKER) build -f ./$(MODULE)/Dockerfile .

generate:
	$(GO) generate

$(COMMIT_ID):
	$(GIT) rev-parse --short HEAD | tr -d '\n' > $(COMMIT_ID)

$(BINARY):  $(COMMIT_ID) $(GOFILES)
	$(GO) build -v -o $(BINARY) ./$(MAIN_PACKAGE)

$(DESCRIPTOR): ModuleDescriptor-template.json
	sed "s/@version@/$(VERSION)/g" $< > $@

check:
	$(GO) test -v -cover -coverpkg=./... -coverprofile=$(COVERAGE) ./...

check-coverage:
	$(GO) run github.com/vladopajic/go-test-coverage/v2@latest --config=./.testcoverage.yaml

run: $(BINARY)
	$(GO) run -buildvcs=true ./$(MAIN_PACKAGE)

docker:
	docker build -t indexdata/mod-dms:latest .

fmt:
	$(GOFMT) -w $(GOFILES)

fmt-check:
	$(GOFMT) -d $(GOFILES)

lint:
	$(GO) run github.com/golangci/golangci-lint/cmd/golangci-lint@latest run

clean:
	rm -f $(BINARY)
	rm -f $(DESCRIPTOR)
	rm -f $(COVERAGE)
	rm -f $(COMMIT_ID)
