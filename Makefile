GO ?= go
SUBDIRS := testutil iso18626 ncip directory directory-mock sru marcxml httpclient zoom illmock broker
TOOL_SUBDIRS := iso18626 ncip directory directory-mock sru marcxml broker
TOOL_SUBDIR_TARGETS := $(TOOL_SUBDIRS:%=%-tools-update)
GOALS := $(or $(MAKECMDGOALS),all)
SUBDIR_GOALS := $(filter-out tools-update,$(GOALS))

.PHONY: $(GOALS) $(SUBDIRS) $(TOOL_SUBDIR_TARGETS) deps-update tools-update

$(SUBDIR_GOALS): .git/hooks/pre-push $(SUBDIRS)

$(SUBDIRS):
	$(MAKE) -C $@ $(SUBDIR_GOALS)

$(TOOL_SUBDIR_TARGETS):
	$(MAKE) -C $(@:-tools-update=) tools-update

deps-update:
	$(GO) work sync

tools-update: .git/hooks/pre-push $(TOOL_SUBDIR_TARGETS)

.git/hooks/pre-push: pre-push
	cp pre-push .git/hooks/pre-push
