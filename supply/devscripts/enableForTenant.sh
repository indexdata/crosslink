#!/bin/bash

TENANT=diku
OKAPI_URL=localhost:9130
VER=99.99.99
SVCID=crosslink-supply-${VER}
INSTID=inst-crosslink-supply-${VER}

PS4="\n\n\n+" # pad set -x with some extra whitespace
set -x # echo commands

# Removing potentially existing previous deployment...
# (using a fixed version for dev deployment works better for this since
# we'll know what it was regardless what branch we were on last time we ran)
curl -XDELETE "${OKAPI_URL}/_/proxy/tenants/${TENANT}/modules/${SVCID}"
curl -XDELETE "${OKAPI_URL}/_/discovery/modules/${SVCID}/${INSTID}"
curl -XDELETE "${OKAPI_URL}/_/proxy/modules/${SVCID}"

# Install anew and enable for tenant
sed "s/@version@/${VER}/g" ../descriptors/ModuleDescriptor-template.json | curl -XPOST ${OKAPI_URL}/_/proxy/modules -d @-
curl -XPOST ${OKAPI_URL}/_/discovery/modules -d @./discover.json
curl -XPOST ${OKAPI_URL}/_/proxy/tenants/${TENANT}'/install' -d @./enable.json
