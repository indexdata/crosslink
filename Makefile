SUBDIRS := iso18626 sru marcxml httpclient illmock broker
GOALS := $(or $(MAKECMDGOALS),all)

.PHONY: $(GOALS) $(SUBDIRS)

$(GOALS): .git/hooks/pre-commit $(SUBDIRS)

$(SUBDIRS):
	$(MAKE) -C $@ $(MAKECMDGOALS)

.git/hooks/pre-commit: pre-commit
	cp pre-commit .git/hooks/pre-commit
