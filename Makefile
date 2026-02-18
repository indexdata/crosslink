GO ?= go
SUBDIRS := iso18626 ncip directory sru marcxml httpclient illmock broker
GOALS := $(or $(MAKECMDGOALS),all)

.PHONY: $(GOALS) $(SUBDIRS) deps-update

$(GOALS): .git/hooks/pre-push $(SUBDIRS)

$(SUBDIRS):
	$(MAKE) -C $@ $(MAKECMDGOALS)

deps-update:
	$(GO) work sync

.git/hooks/pre-push: pre-push
	cp pre-push .git/hooks/pre-push
