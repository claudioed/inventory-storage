-- ADR 0010: optional DOT hazard class (1-9, top-level classes only) on a
-- SKU's ProductClassification. NULL means "unspecified" — meaningful and
-- settable only when 'Hazmat' is present in handling_tags (enforced in the
-- domain layer, not by a DB constraint, consistent with how
-- temperature_class's conditional requirement is not DB-enforced either).
ALTER TABLE product_classifications
    ADD COLUMN dot_hazard_class SMALLINT NULL;
