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

## Running against the real Directory

The same Bruno collection can run with the broker reading entries from the real
Directory service. Illmock continues to provide the NCIP and ISO18626 endpoints.

From the repository root, validate and start this variant with:

```
docker compose \
  -f bruno/docker-compose.yml \
  -f bruno/docker-compose-directory.yml \
  config -q
docker compose \
  -f bruno/docker-compose.yml \
  -f bruno/docker-compose-directory.yml \
  up -d --build
```

The one-shot `directory-seed` service waits for Directory, loads
`bruno/directory.json` through the public API, and verifies the resulting
entries and relationships. Check its status and logs with:

```
docker compose \
  -f bruno/docker-compose.yml \
  -f bruno/docker-compose-directory.yml \
  ps directory-seed
docker compose \
  -f bruno/docker-compose.yml \
  -f bruno/docker-compose-directory.yml \
  logs directory-seed directory broker illmock
```

After the broker is ready, run the collection headlessly:

```
cd bruno/crosslink
npx --yes @usebruno/cli@3.5.2 run \
  --env LocalDev \
  --env-var userPassword="dummy"
```

The seeder deliberately requires an empty, disposable Directory database. Tear
down the stack and its volumes before reseeding or after a partial seed failure:

```
docker compose \
  -f bruno/docker-compose.yml \
  -f bruno/docker-compose-directory.yml \
  down -v --remove-orphans
```

Broker requests use symbol-based tenant mapping and
`directory.system.all`. They intentionally do not forward `X-Okapi-Tenant` to
Directory in this test variant.
