-- +goose Up
CREATE TABLE effectus_kafka_deliveries (
    delivery_id text PRIMARY KEY,
    failures integer NOT NULL DEFAULT 0 CHECK (failures >= 0),
    poison_acknowledged boolean NOT NULL DEFAULT false,
    poison_policy text,
    poison_error text,
    topic text,
    partition_id integer,
    offset_id bigint,
    updated_at timestamptz NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE IF EXISTS effectus_kafka_deliveries;
