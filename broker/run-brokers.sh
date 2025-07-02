#!/bin/sh
rm -f *.log
killall -qw illmock
killall -qw broker
num=3
i=0
while test $i -lt $num; do
	MP=`expr $i + 8080`
	BP=`expr $i + 8090`
	PEER_URL=http://localhost:$BP/iso18626 HTTP_PORT=$MP \
		../illmock/illmock >mock.$i.log 2>&1 &

	MOCK_CLIENT_URL=http://localhost:$MP/iso18626 \
		HTTP_PORT=$BP \
		DB_USER=folio \
		DB_PASSWORD=folio \
		DB_DATABASE=folio_modules \
		DB_PORT=5432 \
		./broker > broker.$i.log 2>&1 &
	i=`expr $i + 1`
done
sleep 1
i=0
num=10
while test $i -lt $num; do
	sed "s/5636c993-c41c-48f4-a285-470545f6f343/`uuidgen`/g" < test/testdata/request-loaned.xml |
		curl -s -HContent-Type:text/xml -XPOST -d@- http://localhost:8080/iso18626 -o out.$i.log &
	i=`expr $i + 1`
done
