# NCIP 1.0 integration

`ncipclientv1.Client` sends and receives native `ncipv1` types over HTTP.
It supports LookupUser, AcceptItem, RequestItem, CancelRequestItem, CheckInItem,
and CheckOutItem. Callers supply initiation headers and identifier agencies.
The client writes namespace-free XML with the NCIP 1.0 DTD version identifier;
it does not load the DTD. Responses must contain a single response service.
Problems retain their native messaging/processing errors in `*ncipclientv1.Error`.

A nil HTTP client uses a 30 second timeout. Responses are limited to 10 MiB
by the shared HTTP client, and XML nesting to 64 levels. `SetLogFunc` must be
called before concurrent use; credentials, authentication inputs, names, and
addresses are redacted in logged messages.

Configure `lmsConfig.ncipVersion: "1.0"` in Directory to select this adapter.
Allowed values are `"1.0"` and `"2.02"`; omission inherits the profile default
(currently `"2.02"` for every supported vendor). Explicit configuration wins over
profile defaults. `ncipNamespaceEnabled` applies only to v2; v1 always uses
namespace-free XML. The Dockerfile already includes `ncipv1`.

`lms.CreateLmsAdapterNcip` and `LmsCreator` select the resolved version.
`lms.CreateLmsAdapterNcip1` remains available for callers explicitly requesting v1.

The adapter handles only fields used by the broker:

| Operation | NCIP 1.0 behavior |
| --- | --- |
| LookupUser | Unique user ID first; on a protocol rejection, retries as `Barcode Id` authentication. Extracts canonical ID, optional name/email and privileges. Uses the same profile eligibility rules as v2. |
| AcceptItem | Broker owns the request and introduced item; LMS owns the user. Pickup overrides header ToAgency, following lib-ncip-client. Sends an indeterminate loan period because the interface provides no return date. |
| RequestItem | Sends one bibliographic ID and requests description/location data. Pickup is not consumed (`RequestItemUsesPickupLocation` returns false). Nonempty item-location constraints return an error. Barcode comes from VisibleItemId or the existing CopyNumber fallback, not the opaque UniqueItemId. |
| CancelRequestItem | Cancels by request ID and user ID. |
| CheckInItem | Sends the LMS item ID; honors the enabled flag. |
| CheckOutItem | Sends user/item and optional request ID. Returns title and a usable due date. External references are unsupported and return an error. |
| DeleteItem | Not part of v1; returns an error unless requester item integration is disabled. |
| CreateUserFiscalTransaction | Returns an error: the LMS interface lacks the action, type, currency and amount required by v1. |

Disabled operations retain the existing skipped-result semantics. Unsupported
operations never report success. No v1/v2 model conversion is used.

Run `make -C ncipv1 generate` and `make -C broker generate` before Go tests.
From `broker`, run `go test ./ncipclientv1 ./lms`. Adapter tests also validate
outgoing requests against the bundled v1 XSD when `xmllint` is available.
Live vendor compatibility and socket-based NCIP transports are outside this adapter.
