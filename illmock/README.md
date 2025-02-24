# Intro

The `illmock` program is a utility for mocking ILL ISO18626 + SRU/OASIS
searchRetrive services.

# ILL service

The ILL protocol handling is triggered by requests to URI path `/iso18626`.

`illmock` can operate as both an ILL requester and an ILL supplier, depending
on the type of ISO18626 message it processes.

Example of launching two `illmock` instances that will send messages to each
other:

    HTTP_PORT=8082 PEER_URL=http://localhost:8081 ./illmock
    HTTP_PORT=8081 PEER_URL=http://localhost:8082 ./illmock

We will use the former as a requester and the latter as a supplier, by sending
a Patron Request to the former with:

    curl -XPOST -HContent-Type:text/xml \ -d'<ISO18626Message><request><bibliographicInfo><supplierUniqueRecordId>WILLSUPPLY_LOANED</supplierUniqueRecordId>
     </bibliographicInfo><serviceInfo><requestType>New</requestType><requestSubType>PatronRequest</requestSubType><serviceType>
     </serviceType></serviceInfo></request></ISO18626Message>' http://localhost:8081/iso18626

The `supplierUniqueRecordId` value is used to invoke a particular scenario on
the supplier.

The scenario is used by the supplier to perform a particular workflow. The
following values are recognized:

    WILLSUPPLY_LOANED
    WILLSUPPLY_UNFILLED
    UNFILLED
    LOANED

The scenario is inspected in the supplier request
`<bibliographicInfo><supplierUniqueRecordId>` field.

# ILL flows

History of ILL messages can be retrieved at the `/api/flows` endpoint.
For example:

    curl http://localhost:8081/api/flows

# SRU service

The program offers an SRU service at URI path `/sru`. Only version 2.0
is supported. It is substantially different from version 1.1, 1.2 -
for example different namespace and different semantics for recordPacking.

The service produces a MARCXML record if a query of format "id = value" is
used. For example to get a MARCXML with identifier 123, use:

    curl 'http://localhost:8081/sru?query=id%3D123'

With yaz-client:

    yaz-client http://localhost:8081/sru
    Z> sru get 2.0
    Z> f id=123
    Z> s

With zoomsh:

    zoomsh "set sru_version 2.0" "set sru get" \
        "connect http://localhost:8081/sru" \
        "search cql:id=123" "show 0 1" "quit"

# Environment variables

## HTTP_PORT

Listen address + port. If empty or omitted, the program will listen on any interface, port `8081`.

If the value includes a colon, it is assumed to be listening address and port, for example: `127.0.0.1:8090`.
Without colon, it translates to `:`value which will bind on any interface and port given.

## PEER_URL

The default value is `http://localhost:8081`.

## AGENCY_TYPE

If omitted or empty, a value of `MOCK` is used.

## SUPPLYING_AGENCY_ID

If omitted or empty, a value of `SUP` is used.

## REQUESTING_AGENCY_ID

If omitted or empty, a value of `REQ` is used.

## CLEAN_TIMEOUT

Flow WS API: Specifies how long a flow is kept in memory before being removed. Default value is `10m`.

# Deploy on Kubernetes

Use the Helm chart published from this repo. You will need a GitHub __classic__
`Personal Access Token` with `read:packages` scope.
Go to your Profile Settings > Developer Settings to get it and save it a file called `TOKEN`.

To login to the GitHub OCI registry:

```
cat TOKEN | helm registry login ghcr.io -u $ --password-stdin
```

Now you can install the chart to your current cluster context/namespace with:

```
helm install crosslink-illmock oci://ghcr.io/indexdata/charts/crosslink-illmock --devel
```

but first you need to ensure the cluster has the `TOKEN` deployed as a secret called
`ghcr-secret` to allow the chart to successfully pull the image:

```
kubectl create secret docker-registry ghcr-secret --docker-server=https://ghcr.io --docker-username=$ --docker-password=$(cat TOKEN)
```

You can configure the `illmock` during install with:

```
--set env.{PARAMETER}={value}
```

The chart uses the `LoadBalancer` service type by default. If you're installing on EKS make sure to expose the LB to the internet with:

```
--set "serviceAnnotations.service\.beta\.kubernetes\.io/aws-load-balancer-type=external" \
--set "serviceAnnotations.service\.beta\.kubernetes\.io/aws-load-balancer-nlb-target-type=instance" \
--set "serviceAnnotations.service\.beta\.kubernetes\.io/aws-load-balancer-scheme=internet-facing"
```
