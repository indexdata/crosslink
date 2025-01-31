SUBDIRS := iso18626 broker illmock
GOALS := $(or $(MAKECMDGOALS),all)

.PHONY: $(GOALS) $(SUBDIRS)

$(GOALS): $(SUBDIRS)

$(SUBDIRS):
	$(MAKE) -C $@ $(MAKECMDGOALS)
