SUBDIRS := iso18626 sru marcxml httpclient illmock broker
GOALS := $(or $(MAKECMDGOALS),all)

.PHONY: $(GOALS) $(SUBDIRS)

$(GOALS): .git/hooks/pre-push $(SUBDIRS)

$(SUBDIRS):
	$(MAKE) -C $@ $(MAKECMDGOALS)

.git/hooks/pre-push: pre-push
	cp pre-push .git/hooks/pre-push
