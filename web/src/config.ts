/** Local-dev base URL for inventory-storage's own REST API. Mirrors
 *  e2e-tests/env.sh's INVENTORY_HTTP_PORT=8082. See warehouse-console's
 *  src/config.ts for the note on swapping to runtime config before
 *  multi-environment deployment. */
export const INVENTORY_API_BASE = "http://localhost:8082";
