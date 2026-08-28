GO ?= go
DOCKER ?= docker
SUBDIRS := testutil iso18626 ncip directory sru marcxml httpclient zoom illmock broker
TOOL_SUBDIRS := iso18626 ncip directory sru marcxml broker
TOOL_SUBDIR_TARGETS := $(TOOL_SUBDIRS:%=%-tools-update)
GOALS := $(or $(MAKECMDGOALS),all)
SUBDIR_GOALS := $(filter-out tools-update clean-build-caches,$(GOALS))

.PHONY: $(GOALS) $(SUBDIRS) $(TOOL_SUBDIR_TARGETS) deps-update tools-update clean-build-caches

$(SUBDIR_GOALS): .git/hooks/pre-push $(SUBDIRS)

$(SUBDIRS):
	$(MAKE) -C $@ $(SUBDIR_GOALS)

$(TOOL_SUBDIR_TARGETS):
	$(MAKE) -C $(@:-tools-update=) tools-update

tools-update: .git/hooks/pre-push $(TOOL_SUBDIR_TARGETS)

clean-build-caches:
	$(GO) clean -cache
	$(DOCKER) builder prune --all --force

.git/hooks/pre-push: pre-push
	cp pre-push .git/hooks/pre-push
