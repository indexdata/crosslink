# Introduction

Crosslink broker manages inter-library loan (ILL) transactions, specifically:

* accepts and handles ISO18626 requests
* locates suppliers via _Search/Retrieval via URL_ (SRU) protocol
* negotiates loans with suppliers via ISO18626
* forwards settled loan notification to requesters

# Configuration

Configuration is provided via environment variables:

| Name                  | Description                                 | Default value                     |
|-----------------------|---------------------------------------------|-----------------------------------|
| HTTP_PORT             | Server port                                 | `8081`                            |
| DB_TYPE               | Database type                               | `postgres`                        |
| DB_USER               | Database user                               | `crosslink`                       |
| DB_PASSWORD           | Database password                           | `crosslink`                       |
| DB_HOST               | Database host                               | `localhost`                       |
| DB_DATABASE           | Database name                               | `crosslink`                       |
| DB_PORT               | Database port                               | `25432`                           |
| ENABLE_JSON_LOG       | Should JSON log format be enabled           | `false`                           |
| INIT_DATA             | Should init test data                       | `true`                            |
| MOCK_CLIENT_URL       | Mock client URL used for directory entries  | `http://localhost:19083/iso18626` |
| HOLDINGS_ADAPTER      | Holdings lookup method: `mock` or `sru`     | `mock`                            |
| SRU_URL               | URL when HOLDINGS_ADAPTER is `sru`          | `http://localhost:8081/sru`       |
| PEER_REFRESH_INTERVAL | Peer refresh interval in seconds            | `300`                             |

# Deploy on Kubernetes

Use the Helm chart published from this repo. You will need a GitHub __classic__
`Personal Access Token` with `read:packages` scope.
Go to your Profile Settings > Developer Settings to get it and save it a file called `TOKEN`.

To login to the GitHub OCI registry:

    cat TOKEN | helm registry login ghcr.io -u $ --password-stdin

Now you can install the chart to your current cluster context/namespace with:

```
helm install crosslink-broker oci://ghcr.io/indexdata/charts/crosslink-broker --devel
```

but first you need to ensure the cluster has the `TOKEN` deployed as a secret called
`ghcr-secret` to allow the chart to successfully pull the image:

```
kubectl create secret docker-registry ghcr-secret --docker-server=https://ghcr.io --docker-username=$ --docker-password=$(cat TOKEN)
```

You can configure the broker during install with:

```
--set env.{PARAMETER}={value}
```

The chart uses the `LoadBalancer` service type by default. If you're installing on EKS make sure to expose the LB to the internet with:

```
--set "serviceAnnotations.service\.beta\.kubernetes\.io/aws-load-balancer-type=external" \
--set "serviceAnnotations.service\.beta\.kubernetes\.io/aws-load-balancer-nlb-target-type=instance" \
--set "serviceAnnotations.service\.beta\.kubernetes\.io/aws-load-balancer-scheme=internet-facing"
```

# Build

Generate sources (from XSD) and compile the main program with:

```
make
```

You can also run included tests with:

```
make check
```

or run test for selected `_test` package

```
go test -v -coverpkg=./.. -cover ./cmd/broker
```

# Run locally

You can run the program locally with:

```
make run
```

Program needs DB. You can start DB in docker. 
There is `docker-compose.yml` file prepared to start DB with default user credentials and default port.

