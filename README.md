# Crosslink ILL services

Crosslink project provides software services for implementation of Inter-Library Loan (ILL) systems.

See individual components docs for details:

* [broker](broker/README.md): ISO18626 transaction broker
* [illmock](illmock/README.md): ISO18626 + SRU mocking service

# Docker and Kubernetes

All containers and Helm charts from this repository are published to the GitHub Container Registry,
`ghcr.io/indexdata`.

The registry, like the repository, is private and you will need a GitHub __classic__
`Personal Access Token` with `read:packages` scope for access.
Go to your Profile Settings > Developer Settings to get it and save it a file called `TOKEN`.

Once the token is issued, login to the registry with with the `docker` CLI:

```
cat TOKEN | docker login ghcr.io --password-stdin --username $
```

Run the container for a given application in this repository with (replace `{appName}` with e.g. `crosslink-broker`):

```
docker run ghcr.io/indexdata/{appName}:main
```

Only `main` tag is available.

If you're running on a platform that is not `linux/amd64`, make sure to pass the `--platform linux/amd64` flag to `docker pull/run`.
Only `amd64` images are published from this repository.

When deploying on Kubernetes, use the Helm charts published from this repo under `ghcr.io/indexdata/charts`.

To login to the GitHub OCI registry with Helm:

```
cat TOKEN | helm registry login ghcr.io -u $ --password-stdin
```

Now you can install the chart to your current cluster context/namespace with (replace `{releaseName}` and `{appName}` with appropriate values):

```
helm install {releaseName} oci://ghcr.io/indexdata/charts/{appName} --devel
```

but first you need to ensure the cluster has the `TOKEN` deployed as a secret called
`ghcr-secret` to allow the chart to successfully pull the image:

```
kubectl create secret docker-registry ghcr-secret --docker-server=https://ghcr.io --docker-username=$ --docker-password=$(cat TOKEN)
```

You can configure environment variables during `helm install` with:

```
--set env.{PARAMETER}={value}
```

Charts use the `LoadBalancer` service type by default, you can change this during installation:

```
--set service.type=ClusterIP
```
