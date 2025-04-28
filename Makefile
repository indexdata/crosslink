SUBDIRS := iso18626 sru marcxml httpclient illmock broker
GOALS := $(or $(MAKECMDGOALS),all)

.git/hooks/pre-commit: pre-commit
	cp pre-commit .git/hooks/pre-commit

.PHONY: $(GOALS) $(SUBDIRS)

$(GOALS): $(SUBDIRS)

$(SUBDIRS):
	$(MAKE) -C $@ $(MAKECMDGOALS)
