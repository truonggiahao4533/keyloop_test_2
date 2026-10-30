-- +goose Up
CREATE TYPE action_type AS ENUM (
  'PRICE_REDUCTION_PLANNED','PRICE_REDUCED','TRANSFER_PLANNED',
  'WHOLESALE_PLANNED','PROMOTION_PLANNED','NO_ACTION_NEEDED');

-- Single-row settings table (the dealership's configuration)
CREATE TABLE app_settings (
  id BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (id),
  aging_threshold_days INT NOT NULL DEFAULT 90
    CHECK (aging_threshold_days BETWEEN 1 AND 3650)
);
INSERT INTO app_settings DEFAULT VALUES;

CREATE TABLE vehicles (
  id UUID PRIMARY KEY,
  vin TEXT NOT NULL UNIQUE,
  make TEXT NOT NULL,
  model TEXT NOT NULL,
  year INT NOT NULL,
  price NUMERIC(12,2),
  received_at DATE NOT NULL,
  status TEXT NOT NULL DEFAULT 'IN_STOCK' CHECK (status IN ('IN_STOCK','SOLD','REMOVED')),
  current_action_type action_type,
  current_action_at TIMESTAMPTZ
);
CREATE INDEX idx_vehicles_age ON vehicles (received_at, id)  WHERE status = 'IN_STOCK';
CREATE INDEX idx_vehicles_mm  ON vehicles (make, model)      WHERE status = 'IN_STOCK';

CREATE TABLE vehicle_actions (
  id UUID PRIMARY KEY,
  vehicle_id UUID NOT NULL REFERENCES vehicles(id),
  action_type action_type NOT NULL,
  note TEXT CHECK (char_length(note) <= 1000),
  created_by TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_actions_vehicle ON vehicle_actions (vehicle_id, created_at DESC);

CREATE TABLE idempotency_keys (
  key TEXT PRIMARY KEY,
  request_hash TEXT NOT NULL,
  response_status INT NOT NULL,
  response_body JSONB NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE idempotency_keys;
DROP TABLE vehicle_actions;
DROP TABLE vehicles;
DROP TABLE app_settings;
DROP TYPE action_type;
