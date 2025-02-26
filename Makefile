SUBDIRS := iso18626 sru marcxml httpclient broker illmock
GOALS := $(or $(MAKECMDGOALS),all)

.PHONY: $(GOALS) $(SUBDIRS)

$(GOALS): $(SUBDIRS)

$(SUBDIRS):
	$(MAKE) -C $@ $(MAKECMDGOALS)
