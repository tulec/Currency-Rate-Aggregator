# Postman

## Import

Import both files into Postman:

1. `Currency Rate Aggregator.postman_collection.json`
2. `Local.postman_environment.json`

Select the `Currency Rate Aggregator - Local` environment.

## Start the service

For requests that do not require history:

```powershell
$env:RATE_SOURCES = 'mock'
go run ./cmd/server
```

For the full collection with PostgreSQL:

```powershell
docker compose up -d postgres
$env:DATABASE_URL = 'postgres://currency:currency@localhost:5433/currency_rates?sslmode=disable'
$env:RATE_SOURCES = 'mock'
go run ./cmd/server
```

Run `Get current rate` before history requests so the service has data to save.

## Folder order

1. Health
2. Rates
3. Conversion
4. History
5. Observability
6. Validation and errors

The collection contains Postman tests for response codes, JSON envelopes,
aggregation rules, metrics, and response time.
