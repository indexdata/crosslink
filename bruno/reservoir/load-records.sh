#!/bin/sh

OKAPI_URL=http://localhost:8082

curl -w'\n' -HContent-type:application/json \
 $OKAPI_URL/reservoir/config/modules -d @marc-transformer-conf.json

curl -HContent-Type:application/json \
 -XPUT $OKAPI_URL/reservoir/config/oai -d'{"transformer":"marc-transformer::cluster_transform"}'

curl -w'\n' -HContent-type:application/json \
 $OKAPI_URL/reservoir/config/modules -d @config-matchkeys-isxn.json

# configure pool
curl -w'\n' -HContent-type:application/json \
 $OKAPI_URL/reservoir/config/matchkeys -d @config-pool-isxn.json

# initialize pool
curl -w '\n' -XPUT $OKAPI_URL/reservoir/config/matchkeys/isxn/initialize

filename=1.mrc

set -x

curl -s \
      -HContent-Encoding:application/octet-stream \
      -T $filename "$OKAPI_URL/reservoir/upload?sourceId=US-RS3&sourceVersion=1&xmlFixing=true&fileName=$filename&localIdPath=%24.marc.fields%5B%2A%5D.001"

curl -s "$OKAPI_URL/reservoir/sru?maximumRecords=1&query=isbn%3D9781172431779"
