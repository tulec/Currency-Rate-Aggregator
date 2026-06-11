-- +goose Up
-- noinspection SqlResolve
CREATE INDEX IF NOT EXISTS idx_rates_currency_fetched_at
ON rates(currency, fetched_at DESC, id DESC);

-- +goose Down
DROP INDEX IF EXISTS idx_rates_currency_fetched_at;
