# NCIP 1.0 integration

`ncipclientv1.Client` sends and receives native `ncipv1` types over HTTP.
It supports LookupUser, AcceptItem, RequestItem, CancelRequestItem, CheckInItem,
and CheckOutItem. Callers supply initiation headers and identifier agencies.
The client writes namespace-free XML with the NCIP 1.0 DTD version identifier;
it does not load the DTD. Responses must contain a single response service,
a complete response header, and either a well-formed Problem or the required
success identifiers and service fields. Empty or mixed acknowledgements are
rejected before recording LMS progress. Optional metadata is not fully XSD
validated. Checkout still tolerates unusable DateDue values after a complete
acknowledgement, returning no due date.
Problems retain their native messaging/processing errors in `*ncipclientv1.Error`.
The broker classifies these as protocol rejections (`PROBLEM`); rejected patron
validation follows the review path to `INVALID_PATRON`. Transport failures remain
`ERROR` results.

A nil HTTP client uses a 30 second timeout. Responses are limited to 10 MiB
by the shared HTTP client, and XML nesting to 64 levels. `SetLogFunc` must be
called before concurrent use; credentials, authentication inputs, names, and
addresses and echoed MessagingErrorValue/ProcessingErrorValue fields are
redacted in logged messages, including nested errors. Non-200 HTTP response
bodies are discarded before returning or logging errors; the typed HTTP error
retains its status code.

Configure `lmsConfig.ncipVersion: "1.0"` in Directory to select this adapter.
Allowed values are `"1.0"` and `"2.02"`; omission inherits the profile default
(`"1.0"` for Horizon, Polaris and Symphony; `"2.02"` for the others). Explicit configuration wins over
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
| DeleteItem | Not part of v1; returns `lms.ErrDeleteItemUnsupported` unless requester item integration is disabled. Requester cleanup records this as skipped and allows return shipment without claiming deletion. |
| CreateUserFiscalTransaction | Returns an error: the LMS interface lacks the action, type, currency and amount required by v1. |

Disabled operations retain the existing skipped-result semantics. Unsupported
operations never report success. No v1/v2 model conversion is used.

Requester return and local-close cleanup record unsupported DeleteItem as an
LMS requester `PROBLEM` notice with `operation: DeleteItem`, `outcome: skipped`,
`reason: unsupported`, and the item identifier. The LMS status and identifiers
are retained; manual cleanup may still be needed. Transport or protocol errors
from a supported deletion service still block return shipment, as does failure
to record the skipped-cleanup notice. Acceptance compensation uses the same
cleanup handling and still reports the original persistence failure.

Returned RequestItem IDs are stored as opaque `ncip1-request:` references in
the existing LMS request-ID field. The reference contains three dot-separated,
unpadded base64url components: agency scheme, agency value, and request value.
Cancellation and checkout restore all three components, including after a
restart or during persistence-failure compensation. New broker initiation IDs
remain literal. Legacy plain stored IDs retain their previous FromAgency
ownership; the original agency cannot be recovered if it was already discarded.
When the response omits the request ID, the broker-owned initiating ID is also
encoded before persistence, so literal IDs containing the reserved prefix remain
usable for cancellation and checkout.

Returned unique item IDs use the same three-component encoding with the
`ncip1-item:` prefix in the existing LMS item-ID field. Lender checkout and
check-in restore the returned agency and item value; the visible barcode stays
separate for display and shipment messages. Items without a stored LMS identity
retain barcode-based behavior. Previously discarded identities cannot be recovered.
Malformed references with either reserved prefix fail before sending an HTTP request.

Requester checkout sends no external reference because the workflow has no
reference value to supply. The v1 adapter continues to reject actual nonempty
external references.

Run `make -C ncipv1 generate` and `make -C broker generate` before Go tests.
From `broker`, run `go test ./ncipclientv1 ./lms`. Adapter tests also validate
outgoing requests against the bundled v1 XSD when `xmllint` is available.
Live vendor compatibility and socket-based NCIP transports are outside this adapter.

Requester acceptance returns an opaque item reference. The broker persists the
LMS-returned unique item identity, or the sent broker-owned identity if the
acknowledgement omits it, and reuses it for checkout, check-in, and cleanup.
The visible barcode remains separate. Legacy stored plain identifiers retain
their existing agency fallback; previously discarded agency information cannot
be recovered.

Response decoding failures expose only a generic invalid-response error.
Parser diagnostics and partially decoded messages are discarded before logging;
transport error chains remain intact.

Acknowledgement headers and returned identities reject duplicate singleton
children at their exact XML paths before unmarshalling. Split or conflicting
agencies and identifier values cannot be merged into a successful acknowledgement;
optional metadata retains its existing lenient decoding.

The v1 adapter preserves the configured request scope name for RequestItem and
CancelRequestItem, including the Sierra profile's `Title` value.
Explicit v1 scope values and shared profile configuration remain unchanged.
