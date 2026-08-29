/** Wire types mirroring inventory-storage's dto.go response shapes
 *  (usableInventoryResponse / reservationResponse / allocationResponse)
 *  exactly -- kept hand-in-sync with the Go DTOs rather than code-generated
 *  for v1; revisit with openapi-typescript once the OpenAPI spec is the
 *  enforced source of truth. */
export interface UsableInventory {
  sku: string;
  usable: number;
}

export interface Allocation {
  stockUnitId: string;
  quantity: number;
}

/** Reservation.Status values -- see reservation.go's Status enum:
 *  ACTIVE, CONFIRMED, REVOKED, EXPIRED. */
export type ReservationStatus = "ACTIVE" | "CONFIRMED" | "REVOKED" | "EXPIRED";

export interface Reservation {
  id: string;
  sku: string;
  quantity: number;
  demandRef: string;
  status: ReservationStatus;
  allocations: Allocation[];
  createdAt: string;
  expiresAt: string;
}
