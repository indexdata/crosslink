GO ?= go
SUBDIRS := testutil iso18626 ncip directory sru marcxml httpclient zoom illmock broker
TOOL_SUBDIRS := directory broker
GOALS := $(or $(MAKECMDGOALS),all)
SUBDIR_GOALS := $(filter-out deps-update-tools,$(GOALS))

.PHONY: $(GOALS) $(SUBDIRS) deps-update deps-update-tools

$(SUBDIR_GOALS): .git/hooks/pre-push $(SUBDIRS)

$(SUBDIRS):
	$(MAKE) -C $@ $(MAKECMDGOALS)

deps-update:
	$(GO) work sync

deps-update-tools: .git/hooks/pre-push $(TOOL_SUBDIRS)

.git/hooks/pre-push: pre-push
	cp pre-push .git/hooks/pre-push
