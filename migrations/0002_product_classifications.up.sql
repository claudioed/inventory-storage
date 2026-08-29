CREATE TABLE product_classifications (
    sku               TEXT PRIMARY KEY,
    handling_tags     TEXT[] NOT NULL,
    temperature_class TEXT NOT NULL DEFAULT ''
);
