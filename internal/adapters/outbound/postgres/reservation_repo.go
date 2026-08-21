package postgres

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/claudioed/inventory-storage/internal/domain/reservation"
	"github.com/claudioed/inventory-storage/internal/domain/shared"
)

// ReservationRepo is a pgxpool-backed implementation of ports.ReservationRepo.
type ReservationRepo struct {
	pool *pgxpool.Pool
}

func NewReservationRepo(pool *pgxpool.Pool) *ReservationRepo {
	return &ReservationRepo{pool: pool}
}

func (r *ReservationRepo) Save(ctx context.Context, res *reservation.Reservation) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	_, err = tx.Exec(ctx, `
		INSERT INTO reservations (id, sku, quantity, demand_ref, status, created_at, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (id) DO UPDATE SET status = EXCLUDED.status
	`, res.ID(), res.SKU().String(), res.Quantity().Int(), res.DemandRef(), string(res.Status()), res.CreatedAt(), res.ExpiresAt())
	if err != nil {
		return err
	}

	for _, alloc := range res.Allocations() {
		_, err = tx.Exec(ctx, `
			INSERT INTO reservation_allocations (reservation_id, stock_unit_id, quantity)
			VALUES ($1, $2, $3)
			ON CONFLICT (reservation_id, stock_unit_id) DO UPDATE SET quantity = EXCLUDED.quantity
		`, res.ID(), alloc.StockUnitID, alloc.Quantity.Int())
		if err != nil {
			return err
		}
	}

	return tx.Commit(ctx)
}

func (r *ReservationRepo) FindByID(ctx context.Context, id string) (*reservation.Reservation, error) {
	var sku, demandRef, status string
	var quantity int
	var createdAt, expiresAt time.Time
	err := r.pool.QueryRow(ctx, `
		SELECT sku, quantity, demand_ref, status, created_at, expires_at
		FROM reservations WHERE id = $1
	`, id).Scan(&sku, &quantity, &demandRef, &status, &createdAt, &expiresAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	rows, err := r.pool.Query(ctx, `SELECT stock_unit_id, quantity FROM reservation_allocations WHERE reservation_id = $1`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var allocations []reservation.Allocation
	for rows.Next() {
		var stockUnitID string
		var allocQty int
		if err := rows.Scan(&stockUnitID, &allocQty); err != nil {
			return nil, err
		}
		q, _ := shared.NewQuantity(allocQty)
		allocations = append(allocations, reservation.Allocation{StockUnitID: stockUnitID, Quantity: q})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	skuVO, _ := shared.NewSKU(sku)
	qty, _ := shared.NewQuantity(quantity)
	return reservation.Rehydrate(id, skuVO, qty, demandRef, allocations, reservation.Status(status), createdAt, expiresAt), nil
}

func (r *ReservationRepo) NextID(_ context.Context) (string, error) {
	return "res-" + uuid.NewString(), nil
}
