GO ?= go
GIT ?= git
GOFMT ?= gofmt "-s"
OS ?= $(shell uname)
DOCKER ?= docker
ifeq (Linux, $(OS))
export HOST_DOCKER_INTERNAL=172.17.0.1
endif
PACKAGES ?= $(shell $(GO) list ./...)
GOFILES := $(shell find . -name "*.go")
BUILD_GOFILES := $(shell find . -name "*.go" ! -name "*_test.go" ! -path "./test/*" ! -name "tools.go")
GOTOOLS=tools.go
BINARY=broker
MAIN_PACKAGE=cmd/broker
MODULE=broker
COMMIT_ID=vcs/commit.txt
GIT_HEAD_FILE := $(shell $(GIT) rev-parse --git-path HEAD 2>/dev/null)
GIT_HEAD_REF := $(shell $(GIT) symbolic-ref -q HEAD 2>/dev/null)
GIT_HEAD_REF_FILE := $(if $(GIT_HEAD_REF),$(shell $(GIT) rev-parse --git-path $(GIT_HEAD_REF) 2>/dev/null))
GIT_COMMIT_DEPS = $(GIT_HEAD_FILE) $(wildcard $(GIT_HEAD_REF_FILE))
COVERAGE=coverage.out
RETURNABLES_JSON=patron_request/service/statemodels/returnables.json
RETURNABLES_YAML=../misc/returnables.yaml
PULLSLIP_TEMPLATE=pullslip/service/pull_slip_template.html

# SQLC
SQLC ?= sqlc
SQLC_CONFIG = sqlc/sqlc.yaml
SQL_GEN_OUT_ILL_DB = ill_db/ill_db_gen.go ill_db/ill_models_gen.go ill_db/ill_query.sql_gen.go
SQL_GEN_OUT_EVENT = events/event_db_gen.go events/event_models_gen.go events/event_query.sql_gen.go
SQL_GEN_OUT_PR = patron_request/db/pr_db_gen.go patron_request/db/pr_models_gen.go patron_request/db/pr_query.sql_gen.go
SQL_GEN_OUT_PS = pullslip/db/ps_db_gen.go pullslip/db/ps_models_gen.go pullslip/db/ps_query.sql_gen.go
SQL_GEN_OUT = $(SQL_GEN_OUT_ILL_DB) $(SQL_GEN_OUT_EVENT) $(SQL_GEN_OUT_PR) $(SQL_GEN_OUT_PS)
SQL_GEN_IN = sqlc/*.sql

# OpenAPI
OAPI_DIR=oapi
OAPI_CFG = $(OAPI_DIR)/cfg.yaml
OAPI_SPEC = $(OAPI_DIR)/open-api.yaml
OAPI_OVERLAY = $(OAPI_DIR)/overlay.yaml
OAPI_GEN = $(OAPI_DIR)/openapi_gen.go

# Patron request OpenAPI
PR_OAPI_DIR=oapi
PR_OAPI_CFG = $(PR_OAPI_DIR)/pr-cfg.yaml
PR_OAPI_SPEC = $(PR_OAPI_DIR)/open-api.yaml
PR_OAPI_OVERLAY = $(PR_OAPI_DIR)/pr-overlay.yaml
PR_OAPI_GEN = patron_request/proapi/pr-openapi_gen.go

# Pull slip OpenAPI
PS_OAPI_DIR=oapi
PS_OAPI_CFG = $(PS_OAPI_DIR)/ps-cfg.yaml
PS_OAPI_SPEC = $(PS_OAPI_DIR)/open-api.yaml
PS_OAPI_GEN = pullslip/oapi/ps-openapi_gen.go

.PHONY: all docker generate generate-sqlc generate-api generate-commit-id check run fmt fmt-check clean view-coverage deps-update deps-update-tools lint vulncheck check-coverage

all: $(BINARY) archive

docker: generate
	cd .. && $(DOCKER) build -t indexdata/$(MODULE):latest -f ./$(MODULE)/Dockerfile .

generate: generate-commit-id generate-sqlc generate-api $(RETURNABLES_JSON)

generate-commit-id: $(COMMIT_ID)

generate-sqlc: $(SQL_GEN_OUT)

generate-api: $(OAPI_GEN) $(PR_OAPI_GEN) $(PS_OAPI_GEN)

$(RETURNABLES_JSON): $(RETURNABLES_YAML)
	$(GO) run github.com/mikefarah/yq/v4@v4.52.5 eval -o=json $< > $@.tmp
	mv -f $@.tmp $@

$(OAPI_GEN): $(OAPI_CFG) $(OAPI_SPEC) $(OAPI_OVERLAY)
	$(GO) run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen -config ./$(OAPI_CFG) ./$(OAPI_SPEC)

$(PR_OAPI_GEN): $(PR_OAPI_CFG) $(PR_OAPI_SPEC) $(PR_OAPI_OVERLAY)
	$(GO) run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen -config ./$(PR_OAPI_CFG) ./$(PR_OAPI_SPEC)

$(PS_OAPI_GEN): $(PS_OAPI_CFG) $(PS_OAPI_SPEC)
	$(GO) run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen -config ./$(PS_OAPI_CFG) ./$(PS_OAPI_SPEC)

$(SQL_GEN_OUT): $(SQL_GEN_IN) $(SQLC_CONFIG)
	$(SQLC) generate -f $(SQLC_CONFIG)

$(COMMIT_ID): $(GIT_COMMIT_DEPS)
	commit="$$( $(GIT) rev-parse --short HEAD )"; \
	if [ ! -f "$@" ] || [ "$$(cat "$@")" != "$$commit" ]; then \
		printf "%s" "$$commit" > "$@"; \
	fi

$(BINARY):  $(COMMIT_ID) $(SQL_GEN_OUT) $(OAPI_GEN) $(PR_OAPI_GEN) $(PS_OAPI_GEN) $(BUILD_GOFILES) $(RETURNABLES_JSON) $(PULLSLIP_TEMPLATE)
	$(GO) build -v -o $(BINARY) ./$(MAIN_PACKAGE)

archive:  $(COMMIT_ID) $(SQL_GEN_OUT) $(OAPI_GEN) $(PR_OAPI_GEN) $(PS_OAPI_GEN) $(BUILD_GOFILES) $(RETURNABLES_JSON) $(PULLSLIP_TEMPLATE)
	$(GO) build -v -o archive ./cmd/archive

check: generate
	$(GO) test -v -cover -coverpkg=./... -coverprofile=$(COVERAGE) ./...

run: $(BINARY)
	$(GO) run -buildvcs=true ./$(MAIN_PACKAGE)

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

check-coverage: check
	$(GO) run github.com/vladopajic/go-test-coverage/v2@latest --config=./.testcoverage.yaml

deps-update:
	$(GO) get -u ./...
	$(GO) mod tidy

deps-update-tools:
	$(GO) get github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@latest
	$(GO) mod tidy

clean:
	$(GO) clean -testcache
	$(GO) clean -cache
	rm -f $(BINARY) archive
	rm -f $(COVERAGE)
	rm -f $(COMMIT_ID)
	rm -f $(SQL_GEN_OUT)
	rm -f $(OAPI_GEN)
	rm -f $(PR_OAPI_GEN)
	rm -f $(PS_OAPI_GEN)
