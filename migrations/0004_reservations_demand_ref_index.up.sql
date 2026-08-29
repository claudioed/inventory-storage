-- Query-by-demand-ref is now a supported pattern (GET /reservations?demandRef=)
-- for the cross-service order-lifecycle console, which needs to answer
-- "what did inventory-storage do for order X, line N". A demand_ref can
-- have multiple reservations over its lifetime (revoke + retry), so this
-- backs a FindByDemandRef query, not a uniqueness constraint.
CREATE INDEX idx_reservations_demand_ref ON reservations (demand_ref);
