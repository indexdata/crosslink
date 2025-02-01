# Intro

The `illmock` program is a mocking ISO18626 client / server utility.

`illmock` can function as both a requester and a supplier, depending on the type of ISO18626 message it processes.

Example of launching two `illmock` instances that will send messages to each other:

    HTTP_PORT=8082 PEER_URL=http://localhost:8081 ./illmock
    HTTP_PORT=8081 PEER_URL=http://localhost:8082 ./illmock

We will use the former as a requester and the latter as a supplier, by sending a Patron Request
to the former with:

    curl -XPOST -HContent-Type:text/xml \ -d'<ISO18626Message><request><bibliographicInfo><supplierUniqueRecordId>WILLSUPPLY_LOANED</supplierUniqueRecordId>
     </bibliographicInfo><serviceInfo><requestType>New</requestType><requestSubType>PatronRequest</requestSubType><serviceType>
     </serviceType></serviceInfo></request></ISO18626Message>' http://localhost:8081/iso18626

The `supplierUniqueRecordId` value is used to invoke a particular scenario on the supplier.

## Scenarios

The scenario is used by the supplier to perform a particular workflow. The following values are recognized:

    WILLSUPPLY_LOANED
    WILLSUPPLY_UNFILLED
    UNFILLED
    LOANED

The scenario is inspected in the supplier request `<bibliographicInfo><supplierUniqueRecordId>` field.
It may be also given at startup with the environment variable `AGENCY_SCENARIO` which will launch the
supplier request automatically to the peer specified with `PEER_URL`.

## Environment variables

### HTTP_PORT

Listen address + port. If empty or omitted, the program will listen on any interface, port `8081`.

If the value includes a colon, it is assumed to be listening address and port, for example: `127.0.0.1:8090`.
Without colon, it translates to `:`value which will bind on any interface and port given.

### PEER_URL

The default value is `http://localhost:8081`.

### AGENCY_SCENARIO

If `AGENCY_SCENARIO` is defined and non-empty the `illmock` program will initiate requests during start (100 ms after launch).
See [Scenario].

### AGENCY_TYPE

If omitted or empty, a value of `MOCK` is used.

### SUPPLYING_AGENCY_ID

If omitted or empty, a value of `SUP` is used.

### REQUESTING_AGENCY_ID

If omitted or empty, a value of `REQ` is used.

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
