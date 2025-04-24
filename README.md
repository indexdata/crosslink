# Introduction and API

mod-dms is a simple webservice exposing two operations against an S3 bucket:

- `POST` to `/upload` a body encoded as `multipart/form-data` with a file to be uploaded in the field named `file` will store the file as an object in the configured S3 bucket with the content type specified on that field and return a JSON object body containing a `key` property with the stored object name and `url` property with the full URL to that object.

- `DELETE` to `/upload/<object name>` will remove the object

Objects are named with a UUID and optionally prefixed with the contents of the `X-Okapi-Tenant` header.

# Configuration

Configuration is through the following environment variables:

- `HOST` (default `` equivalent to any)

- `HTTP_PORT` (default `8086`)

- `LOG_JSON`, if present and set to `true`, indicates to output structured logs as `JSON`

- `LOG_LEVEL` (default `info`), sets log level to one of `debug, info, warn, error`

- `MOD_DMS_BUCKET` is required and consists of five comma-delimited components: bucket,region,endpoint,access,secret

- `MOD_DMS_TYPES` optionally provides a comma-delimited list of content-types to accept

- `MOD_DMS_INSECURE`, if present and set to `true`, indicates to use `http` rather than `https` to access the configured endpoint
