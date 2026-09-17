# Bruno

This directory files for running tests with Bruno.

## Patron Request API E2E test

The provided Bruno API test includes an end-to-end execution of a happy path exchange between the requester (borrower) and supplier (lender).

To execute it:

1. In the current directory, start the broker service along with its dependencies (Postgres) and mock Directory, ISO18626 and NCIP services:

```
docker compose up
```

2. Launch Bruno and open the `crosslink` collection located in this directory.

3. In Bruno, load the `LocalDev` environment.

4. Run all steps in the Bruno runner in the `PR Happy Flow` folder. All HTTP response codes and validations should be green.

The happy path ships with a past due date, receives the item, explicitly invokes supplier `overdue`, requests renewal, and accepts it with a future date before completing checkout/check-in and return. It checks `OVERDUE`, `RENEWAL_PENDING`, `RENEWED`, and due dates on both sides without waiting for the scheduler. Dates are calculated relative to the run time.

The separate `Open-ended loan` folder ships without a date and completes receipt and return, checking that neither side has a due date. This requires the local mock's undated checkout responses and absent `defaultLoanPeriod`.

`Renewal rejection and open-ended acceptance` checks that rejection returns both sides to `OVERDUE` with the original date, then requests renewal again and accepts without a date. `Renewal null date` accepts with explicit `dueDate: null`. Both verify that acceptance clears the date on each side and complete the return workflow.

Run all scenarios exactly as CI does:

```sh
cd crosslink
npx --yes @usebruno/cli@3.5.2 run --env LocalDev --env-var userPassword=dummy
```

## Condition rejection regression

The `PR Condition rejection` folder creates a Loan with two suppliers and detects
which supplier is selected first. It adds a condition, rejects it on the borrowing
side, and accepts cancellation on the lending side. It verifies that the requester
continues with the other supplier and that a distinct lending request exists there.
It then repeats condition rejection and cancellation with that second supplier,
verifying that the exhausted rota leaves the requester terminal `UNFILLED`.
Both rounds check requester `CANCEL_PENDING`, supplier `CANCEL_REQUESTED`, and
supplier terminal `CANCELLED` states.

Run it with the same stack and environment:

```sh
cd crosslink
npx --yes @usebruno/cli@3.5.2 run "PR Condition rejection" --env LocalDev --env-var userPassword="dummy"
```

The existing CI collection run includes this folder automatically. Both environments
provide `secondSupplierSymbol` and `OkapiTenantSup2` for RS2. The scenario uses
separate request variables from the happy flow. Continuation must succeed before
this scenario can verify exhaustion.

## Reservoir (incomplete)

This is similar to the E2E test with the twist that it uses holdings SRU lookup.

The docker compose file `docker-compose-reservoir.yml` assumes reservoir in `../../reservoir`, ie `crosslink` and `reservoir` side by side.

Start with:

```
docker compose -f docker-compose-reservoir.yml up
```

Load at least one MARC for reservoir with the `isxn` matcher with:

```
cd reservoir
./load-records.sh
```

Start Bruno and load the `LocalDev` environment.

Select the `Reservoir` folder in Bruno. Only the first parts of the Happy flow is currently in this holder. This
is merely to test that SRU lookup is operational.
