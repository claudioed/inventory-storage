# REST API (inbound adapter)

- `POST /stock/receive`                        -> ReceiveStock
- `POST /stock/stow`                           -> StowStock
- `POST /reservations`                         -> ReserveStock
- `GET  /reservations?demandRef=`              -> GetReservationsByDemandRef
- `DELETE /reservations/{id}`                  -> RevokeReservation
- `POST /reservations/{id}/confirm-pick`       -> ConfirmPick
- `GET  /inventory/{sku}/usable`               -> GetUsable
- `POST /bins/{binId}/cycle-count`             -> RunCycleCount
- `PUT  /products/{sku}/classification`        -> ClassifyProduct
- `GET  /products/{sku}/classification`        -> current ProductClassification
- `GET  /healthz`

JSON DTOs live in the http adapter; never leak domain structs.

`GET /reservations?demandRef=` is the read side backing the fleet's
cross-service Order Lifecycle console screen — see ADR-0002 in
`warehouse-ops-agent`'s docs and this repo's own adoption-record ADR under
`docs/docs/adr/`. It returns every Reservation ever created against a
caller-supplied `demandRef` (array, since a demandRef can have multiple
reservations across its lifetime — a revoke followed by a retry), never
404s on an unknown demandRef (200 + empty array instead), and is
side-effect-free.

## CORS

`go-chi/cors` middleware is enabled on every route, allowing
`CORS_ALLOWED_ORIGINS` (env, default
`http://localhost:5173,http://localhost:5182` — the `warehouse-console`
shell and this service's own `inventory-mfe` remote).

## Source of truth for the generated reference

`apis/openapi.yaml` is Spectral-linted (`.spectral.yaml`) and is the ONLY
source `docs/docs/api-reference/rest/*.api.mdx` should ever be regenerated
from (`npm run gen-api-docs` in `docs/`, wired to `docusaurus.config.ts`'s
`docusaurus-plugin-openapi-docs` plugin, `specPath: '../apis/openapi.yaml'`).
Never hand-edit the generated `*.api.mdx` / `*.ParamsDetails.json` /
`*.RequestSchema.json` / `*.StatusCodes.json` files under
`docs/docs/api-reference/rest/` — they are build output.
