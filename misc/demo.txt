#Public

1. The broker requires peers to be configured via the JSON `/peer` endpoint or to be available in an external web service via the Directory API.
   For this demo, we will use a pre-configured peer URL so that every peer in our test resolves to the ILL mock URL.
   We could also post the definitions to the /peer endpoint:

  https://broker.crosslink-dev.indexdata.com/peers

2. ILL transactions are created in the broker with ISO18626 Request messages and the broker uses a designated SRU endpoint to lookup holdings.
   The ILL mock includes an test SRU endpoint that returns auto-generated holdings:

  https://illmock.crosslink-dev.indexdata.com/sru?query=id=UNFILLED%3bLOANED&maximumRecords=100

   In this demo, we will first create a patron request in the ILL mock which will then create a loan request in the broker:

  - pre-canned request with one supplier which does not supply:

  curl https://illmock.crosslink-dev.indexdata.com/iso18626 -Hcontent-type:application/xml -d @request-mock-unfilled.xml

  - pre-canned request with 3 suppliers where the 3rd one supplies:

  curl https://illmock.crosslink-dev.indexdata.com/iso18626 -Hcontent-type:application/xml -d @request-mock-multiple.xml

3. After posting requests we can look at the mock `flows` web service to see messages generated on the requester and the supplier side:

  https://illmock.crosslink-dev.indexdata.com/api/flows

4. We can use the `/ill_transactions` endpoint to monitor ILL transactions and their status in the broker:

  https://broker.crosslink-dev.indexdata.com/ill_transactions

  or, for a specific transaction:

  https://broker.crosslink-dev.indexdata.com/ill_transactions/{transactionId}

5. We can also use the `/events` endpoint to monitor all events (including ILL messages sent and received) for a given transaction:

  https://broker.crosslink-dev.indexdata.com/events?ill_transaction_id={transactionId}


#Local

Launch broker on default port and mock on port 8083. Launch Postgres with:

  cd broker/
  docker compose up

Connect to Postgres with:

  psql -U crosslink -p 25432 -h localhost

1. post request to mock, remember to update ID:

  curl -L 'http://localhost:19083/iso18626' -Hcontent-type:application/xml -d @request-mock-multiple.xml

2. show transaction in the DB:

  select timestamp, id, requester_symbol, requester_request_id, last_requester_action, prev_requester_action from ill_transaction order by timestamp;

3. list events for the transaction:

  select timestamp, event_type, event_name, event_status, event_data, result_data from event where ill_transaction_id='a7ed208a-fc35-4ad9-ae57-8d8c40c43993'  order by timestamp;

4. list suppliers for the transaction

  select peer.symbol, supp.ordinal, supp.last_action, supp.last_status, supp.supplier_status from located_supplier supp join peer as peer on supp.supplier_id=peer.id where supp.ill_transaction_id='a7ed208a-fc35-4ad9-ae57-8d8c40c43993'  order by supp.ordinal;
