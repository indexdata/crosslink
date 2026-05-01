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
