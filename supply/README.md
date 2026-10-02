# CrossLink Supply

## API

Supply is a simple webservice exposing two operations against an S3 bucket:

- `POST` to `/dms/upload` a body encoded as `multipart/form-data` with a file to be uploaded in the field named `file` will store the file as an object in the configured S3 bucket with the content type specified on that field and return a JSON object body containing a `key` property with the stored object name and `url` property with the full URL to that object.

- `DELETE` to `/dms/upload/<object name>` will remove the object

Objects are named with a UUID and optionally prefixed with the contents of the `X-Okapi-Tenant` header. Without that header, deletion accepts only a bare UUID. With it, deletion requires exactly `<matching tenant>/<uuid>`. Tenants must be single path components without whitespace, control characters, slashes, or backslashes, and cannot be `.` or `..`.

Use the returned URL directly. When constructing a DELETE URL, encode each object-key path component so reserved characters such as `?`, `#`, and `%` remain part of the key.

Successful uploads return HTTP 200 with `Content-Type: application/json`; successful deletions return HTTP 200, including when the object is already absent. Uploads exceeding the total request limit return 413, malformed multipart requests or invalid keys/tenants return 400, unsupported content types return 415, and storage failures return 503 without an upload key or URL.

## Configuration

Configuration is through the following environment variables:

- `HOST` (default `` equivalent to any)

- `HTTP_PORT` (default `8086`)

- `LOG_JSON`, if present and set to `true`, indicates to output structured logs as `JSON`

- `LOG_LEVEL` (default `info`), sets log level to one of `debug, info, warn, error`

- `MOD_DMS_BUCKET` is required and consists of exactly five comma-delimited components: bucket,region,endpoint,access,secret. Commas inside a component cannot be represented in this format; any other component count or an invalid endpoint prevents startup

- `MOD_DMS_TYPES` optionally provides a comma-delimited list of content-types to accept

- `MOD_DMS_MAX_UPLOAD_BYTES` (default `104857600`, 100 MiB) caps the complete upload request, including multipart overhead and additional fields. Configure a positive decimal byte count; invalid values prevent startup. The file itself must fit within this limit together with multipart overhead.

- `MOD_DMS_INSECURE`, if present and set to `true`, indicates to use `http` rather than `https` to access the configured endpoint

Returned object URLs are unsigned and use the configured storage endpoint. Consumers must be able to reach that endpoint and read the uploaded objects anonymously (for example, through a bucket policy granting `s3:GetObject`). Upload credentials alone do not grant access to these URLs; a private bucket can accept uploads while returned URLs remain inaccessible. Supply does not proxy or presign downloads.

## Building and testing

Supply is a separate Go module at `github.com/indexdata/crosslink/supply`. From the CrossLink repository root:

```bash
make -C supply all
make -C supply check
make -C supply lint
make -C supply docker
```

Tests use a MinIO container and require Docker. The fixture in `test/minio/Dockerfile` builds from the official `RELEASE.2025-03-12T18-04-18Z` GitHub binary with a pinned SHA-256 checksum for amd64 or arm64. Its first build requires access to Docker Hub, Alpine package repositories, and GitHub release downloads. `make -C supply all` produces the `supply` executable and `ModuleDescriptor.json`. The descriptor defaults to development version `99.99.99`; override it with `VERSION=<version>`. The source template is [descriptors/ModuleDescriptor-template.json](descriptors/ModuleDescriptor-template.json).

Configure the environment variables above and start the service with `./supply/supply` from the repository root. `GET /healthz` is the health endpoint.

## Deployment

The shared CrossLink pipeline tests Supply, publishes images for `linux/amd64` and `linux/arm64`, and then publishes its Helm chart. Image tags include `main` and `sha-<short-sha>`. Published charts use the corresponding SHA image tag.

Run the published image with a local environment file containing the configuration above:

```bash
docker run --rm -p 8086:8086 --env-file supply.env ghcr.io/indexdata/crosslink-supply:main
```

Keep environment files containing credentials out of source control.

Install the chart, with bucket credentials provided through an existing Kubernetes Secret:

```bash
helm install crosslink-supply oci://ghcr.io/indexdata/charts/crosslink-supply --devel \
  --set envSecrets.MOD_DMS_BUCKET.name=supply-config \
  --set envSecrets.MOD_DMS_BUCKET.key=bucket
```

The `bucket` secret key must contain the five comma-delimited components documented above. Configure other environment variables through `env`, `envSecrets`, or `envConfigMaps`. The chart defaults to a ClusterIP service on port 80, forwarding to container port 8086. Its readiness and liveness probes use `/healthz`. Private registries can use `imagePullSecrets`. Set `replicaCount` for fixed scaling; the chart does not provide autoscaling.

The application relies on Okapi for authorization. Restrict access to a trusted Okapi route: a tenant header is not proof of authorization, and ClusterIP does not authenticate callers within the cluster. Standalone deployments must provide their own access controls. Direct external exposure requires an explicit `--set service.type=LoadBalancer` override.

SIGINT or SIGTERM stops new connections and allows active requests up to 20 seconds to finish. After that deadline, remaining requests and their storage operations are canceled.

Okapi registration hooks are enabled by default, using module ID `crosslink-supply-<version>` and module URL `http://crosslink-supply:80`. Set `okapi-hooks.moduleUrl` to the actual Service URL when using a different release name, namespace, or fullname override. Disable registration with `--set okapi-hooks.enabled=false` when running without Okapi.

## Migrating from mod-dms

Supply replaces the standalone mod-dms service. Update image references to `ghcr.io/indexdata/crosslink-supply`, chart references to `oci://ghcr.io/indexdata/charts/crosslink-supply`, and Okapi module/discovery registrations to `crosslink-supply-<version>`. Replace the old tenant module registration with the new module registration as part of deployment; update Service URLs where needed.

Existing clients continue to use `/dms/upload`, the `dms` interface and `dms.upload.*` permissions. Existing `MOD_DMS_*` configuration and generated object keys remain supported. Tenant-prefixed deletion now requires the matching tenant header, and uploads default to the 100 MiB total request cap. The chart now defaults to ClusterIP and no longer advertises unsupported autoscaling options; remove old `autoscaling` overrides and use `replicaCount`. No bucket or object migration is required.

The scripts under `devscripts/` register the development version `99.99.99`; run them from that directory after configuring their tenant, Okapi URL, and discovery URL for the local environment.
