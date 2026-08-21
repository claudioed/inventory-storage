CREATE TABLE bins (
    id       TEXT PRIMARY KEY,
    capacity INTEGER NOT NULL CHECK (capacity > 0),
    occupied INTEGER NOT NULL CHECK (occupied >= 0)
);

CREATE TABLE stock_units (
    id       TEXT PRIMARY KEY,
    sku      TEXT NOT NULL,
    bin_id   TEXT NOT NULL REFERENCES bins (id),
    quantity INTEGER NOT NULL CHECK (quantity >= 0),
    reserved INTEGER NOT NULL CHECK (reserved >= 0),
    state    TEXT NOT NULL
);

CREATE INDEX idx_stock_units_sku ON stock_units (sku);
CREATE INDEX idx_stock_units_bin_id ON stock_units (bin_id);

CREATE TABLE reservations (
    id         TEXT PRIMARY KEY,
    sku        TEXT NOT NULL,
    quantity   INTEGER NOT NULL CHECK (quantity > 0),
    demand_ref TEXT NOT NULL,
    status     TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE reservation_allocations (
    reservation_id TEXT NOT NULL REFERENCES reservations (id),
    stock_unit_id  TEXT NOT NULL REFERENCES stock_units (id),
    quantity       INTEGER NOT NULL CHECK (quantity > 0),
    PRIMARY KEY (reservation_id, stock_unit_id)
);

CREATE TABLE events (
    id          BIGSERIAL PRIMARY KEY,
    event_name  TEXT NOT NULL,
    occurred_at TIMESTAMPTZ NOT NULL,
    payload     JSONB NOT NULL
);
