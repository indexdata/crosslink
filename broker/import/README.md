# Import API

The import API loads patron-request aggregates, batch actions, and templates from a newline-delimited JSON (NDJSON) stream.

## Migration export scripts

The [`migration`](../../migration) folder contains SQL scripts that export a mod-rs tenant as NDJSON accepted by the CrossLink import APIs:

1. [`export-crosslink-directory.sql`](../../migration/export-crosslink-directory.sql) exports directory entries, tiers, and networks for [`POST /directory/import`](../../directory/README.md#import-api).
2. [`export-crosslink-config.sql`](../../migration/export-crosslink-config.sql) exports templates and compatible scheduled actions for `POST /import`.
3. [`export-crosslink-open-patron-requests.sql`](../../migration/export-crosslink-open-patron-requests.sql) exports open borrowing and lending requests for `POST /import`.

Run the scripts in this order so directory peers exist before configuration and patron requests are imported. Each script documents its required `psql` variables, export command, validation, and migration limitations.

## Request

```http
POST /import?conflictPolicy=fail
Content-Type: application/x-ndjson
```

`conflictPolicy` is optional and defaults to `fail`. Its supported values are `fail`, `skip`, and `update`.

The request body must contain one complete JSON object per line. Blank lines are ignored. Do not wrap the records in a JSON array and do not pretty-print a record across multiple lines.

Each line has this envelope:

```json
{"type":"template","owner":"ISIL:US-RS1","data":{}}
```

| Field | Meaning |
| --- | --- |
| `type` | `patronRequest`, `batchAction`, or `template` |
| `owner` | Symbol of the institution that owns the imported resource; it must resolve to a known peer |
| `data` | A JSON object matching the schema for the selected resource type |

Unknown envelope fields and unknown fields inside `data` are rejected. The authoritative schemas are `ImportResourceRecord`, `ImportPatronRequestBundle`, `CreateBatchAction`, and `CreateTemplate` in [`broker/oapi/open-api.yaml`](../oapi/open-api.yaml).

### Import a file with curl

```sh
curl --fail-with-body \
  -X POST \
  -H 'Content-Type: application/x-ndjson' \
  --data-binary @resources.ndjson \
  'http://localhost:8081/import?conflictPolicy=fail'
```

Use `--data-binary`, rather than `-d`, so curl preserves the line boundaries. Change the host and port to match the deployment.

### Mixed-resource example

Save the following as `resources.ndjson`. Each record remains on one physical line.

```ndjson
{"type":"template","owner":"ISIL:US-RS1","data":{"title":"Request reminder","purpose":"email","subject":"Request update","body":"Your request has been updated.","contentType":"text","labels":["request-reminder"],"audience":"patron"}}
{"type":"batchAction","owner":"ISIL:US-RS1","data":{"schedule":"FREQ=DAILY;BYHOUR=6;BYMINUTE=0","actionName":"request-aging","title":"Daily request aging","batchQuery":"state==NEW","actionParams":{"interval":"24h"}}}
{"type":"patronRequest","owner":"ISIL:US-RS1","data":{"patronRequest":{"id":"US-RS1-1001","createdAt":"2026-09-15T08:00:00Z","updatedAt":"2026-09-15T08:00:00Z","illRequest":{"header":{"requestingAgencyId":{"agencyIdType":{"#text":"ISIL"},"agencyIdValue":"US-RS1"},"supplyingAgencyId":{"agencyIdType":{"#text":"ISIL"},"agencyIdValue":"US-RS2"},"timestamp":"2026-09-15T08:00:00Z","requestingAgencyRequestId":"US-RS1-1001"},"bibliographicInfo":{"title":"Example title"},"serviceInfo":{"serviceType":"Loan"}},"state":"SENT","stateModel":"default","side":"borrowing","requesterSymbol":"ISIL:US-RS1","supplierSymbol":"ISIL:US-RS2","requesterRequestId":"US-RS1-1001","needsAttention":false},"items":[],"notifications":[],"locatedSuppliers":[]}}
```

The example assumes that `ISIL:US-RS1` is a known peer and that `SENT` is valid for the deployment's `default` borrowing/loan state model.

## Resource details

### Templates

Required `data` fields are `title`, `purpose`, `body`, `contentType`, and a non-empty `labels` array.

- `purpose`: `email` or `pullslip`
- `contentType`: `text` or `html`
- optional `audience`: `patron` or `staff`; omit it for a generic template

Example:

```ndjson
{"type":"template","owner":"ISIL:US-RS1","data":{"title":"Ready for pickup","purpose":"email","subject":"Your item is ready","body":"Please collect {{.Title}}.","contentType":"text","labels":["received-notification"],"audience":"patron"}}
```

### Batch actions

Required `data` fields are `schedule`, `actionName`, `title`, and `batchQuery`. `schedule` is an RRULE expression and `batchQuery` is a CQL query. Supported action names are `email-pullslips` and `request-aging`.

For `request-aging`, `actionParams.interval` is required by the action and uses a Go duration such as `24h` or `168h`.

Example:

```ndjson
{"type":"batchAction","owner":"ISIL:US-RS1","data":{"schedule":"FREQ=WEEKLY;BYDAY=MO;BYHOUR=6;BYMINUTE=0","actionName":"request-aging","title":"Weekly request aging","batchQuery":"state==NEW","actionParams":{"interval":"168h"}}}
```

### Patron requests

A patron-request record imports an aggregate:

- `patronRequest` — required root object
- `items` — required array
- `notifications` — required array
- `locatedSuppliers` — required array
- `illTransaction` — optional; allowed only for borrowing requests

Important validation rules include:

- `createdAt` and `updatedAt` must be timestamps, and `needsAttention` must be present.
- The request's state must exist in `stateModel` for its service type and side.
- `requesterRequestId` must equal `illRequest.header.requestingAgencyRequestId`.
- `requesterSymbol` must match the requesting-agency symbol in the ISO 18626 header.
- For a borrowing request, `id` must equal `requesterRequestId`, and the requester must be the envelope owner or one of its branches.
- For a lending request, `supplierSymbol` is required, must match the supplying-agency symbol in the ISO 18626 header, and must be the envelope owner or one of its branches.
- An `illTransaction`, when supplied, must use the same requester symbol and requester request ID. `locatedSuppliers` require an `illTransaction`.
- Item, notification, and located-supplier IDs must be unique within their respective arrays. At most one located supplier may have status `selected`.

Minimal borrowing example:

```ndjson
{"type":"patronRequest","owner":"ISIL:US-RS1","data":{"patronRequest":{"id":"US-RS1-1001","createdAt":"2026-09-15T08:00:00Z","updatedAt":"2026-09-15T08:00:00Z","illRequest":{"header":{"requestingAgencyId":{"agencyIdType":{"#text":"ISIL"},"agencyIdValue":"US-RS1"},"supplyingAgencyId":{"agencyIdType":{"#text":"ISIL"},"agencyIdValue":"US-RS2"},"timestamp":"2026-09-15T08:00:00Z","requestingAgencyRequestId":"US-RS1-1001"},"bibliographicInfo":{"title":"Example title"},"serviceInfo":{"serviceType":"Loan"}},"state":"SENT","stateModel":"default","side":"borrowing","requesterSymbol":"ISIL:US-RS1","supplierSymbol":"ISIL:US-RS2","requesterRequestId":"US-RS1-1001","needsAttention":false},"items":[],"notifications":[],"locatedSuppliers":[]}}
```

## Conflict policies

The selected policy applies independently to every record in the stream. A record-level conflict or validation error does not stop later records, and successful earlier records remain committed. Every imported resource is written in its own database transaction.

### `fail` (default)

If the resource identity already exists, that record is not changed. It increments the resource's `failed` count and adds a detail to `errors`; processing then continues with the next line.

```sh
curl --fail-with-body \
  -H 'Content-Type: application/x-ndjson' \
  --data-binary @resources.ndjson \
  'http://localhost:8081/import'
```

Example: if owner `ISIL:US-RS1` already has a batch action titled `Daily request aging`, importing another batch action with that owner and title fails, even if its schedule is different.

### `skip`

If the resource identity already exists, the existing resource is left unchanged. The record increments `skipped`, and a diagnostic is included in `errors` so the caller can identify the skipped line.

```sh
curl --fail-with-body \
  -H 'Content-Type: application/x-ndjson' \
  --data-binary @resources.ndjson \
  'http://localhost:8081/import?conflictPolicy=skip'
```

Example: suppose this template already exists:

```ndjson
{"type":"template","owner":"ISIL:US-RS1","data":{"title":"Original title","purpose":"email","body":"Original body","contentType":"text","labels":["request-reminder"],"audience":"patron"}}
```

Importing the following with `skip` finds the overlapping `request-reminder` label but keeps the original title and body:

```ndjson
{"type":"template","owner":"ISIL:US-RS1","data":{"title":"Replacement title","purpose":"email","body":"Replacement body","contentType":"text","labels":["request-reminder"],"audience":"patron"}}
```

### `update`

If exactly one matching resource exists, its mutable data is replaced by the incoming data. The record increments `imported`, not a separate updated count.

```sh
curl --fail-with-body \
  -H 'Content-Type: application/x-ndjson' \
  --data-binary @resources.ndjson \
  'http://localhost:8081/import?conflictPolicy=update'
```

Using `update` with the template example above preserves the existing database ID and creation timestamp, but changes its title, body, and other submitted template fields.

Updates still fail when applying them would make identity resolution unsafe. Examples include changing the immutable identity of a patron request, matching multiple templates, or reusing a nested aggregate ID that belongs to a different aggregate.

### How a conflict is identified

| Resource | Conflict identity | `update` behavior |
| --- | --- | --- |
| Borrowing patron request | Patron-request `id` | Replaces the root data and synchronizes the submitted aggregate collections. The requester request ID, side, and requester owner symbol must still match the existing aggregate. |
| Lending patron request | Patron-request `id`, or the routing identity `(supplierSymbol, requesterRequestId)` | Updates the matched local aggregate, preserving its local ID when the match was by routing identity. The requester request ID, side, and supplier owner symbol must still match. If the incoming ID and routing identity match two different aggregates, the record fails under every policy. |
| Template | Any label overlap for the same `owner`, `purpose`, and `audience` | Updates the one matching template while preserving its ID and creation timestamp. If the labels overlap more than one template, the record fails as ambiguous. A generic template (no audience) and an audience-specific template do not conflict. |
| Batch action | Same `owner` and `title` among batch-action scheduled tasks | Updates that batch action while preserving its ID and creation timestamp, then notifies the scheduler. A different kind of scheduled task with the same title does not conflict. |

For a patron-request update, the submitted arrays are the desired final contents:

- Existing items, notifications, and located suppliers absent from the incoming arrays are deleted.
- Existing entries with matching IDs are updated, and new IDs are inserted.
- An existing associated ILL transaction cannot be omitted from the update.
- An item, notification, located supplier, or ILL transaction already owned by another aggregate causes the entire record transaction to roll back.

For example, updating a patron request with `"notifications":[]` removes all of that request's existing notifications. Use `skip` rather than `update` when the import must never remove or replace existing aggregate data.

## Response

A completed stream returns `200 OK`, even when individual records failed or were skipped. For example, an import using `conflictPolicy=skip` could return:

```json
{
  "patronRequests": {"imported": 1, "failed": 0, "skipped": 0},
  "batchActions": {"imported": 0, "failed": 0, "skipped": 1},
  "templates": {"imported": 1, "failed": 0, "skipped": 1},
  "errors": [
    {
      "line": 2,
      "type": "batchAction",
      "owner": "ISIL:US-RS1",
      "identifier": "Daily request aging",
      "error": "batch action \"Daily request aging\" already exists"
    },
    {
      "line": 4,
      "type": "template",
      "owner": "ISIL:US-RS1",
      "identifier": "request-reminder",
      "error": "template labels overlap existing template \"68d48b0b-8781-41b5-bfea-a7c86f37194d\""
    }
  ],
  "errorsOmitted": 0
}
```

`line` is the one-based physical line number in the NDJSON body, including blank lines. At most 100 error or skip details are retained; `errorsOmitted` reports any additional details. Counters still include all recognized records.

Always inspect the counters and `errors` array instead of treating HTTP 200 as proof that every record was imported.

## Request-level errors and limits

| Status | Meaning |
| --- | --- |
| `400 Bad Request` | Missing body, unsupported `conflictPolicy`, or a content type other than `application/x-ndjson` |
| `413 Request Entity Too Large` | The whole body exceeds 2 GiB or one NDJSON record exceeds 1 MiB |
| `500 Internal Server Error` | The server could not read the request stream |

The handler sets 10-minute read and write deadlines. A fatal stream error such as an oversized record stops reading the request; records already committed before that error are not rolled back.
