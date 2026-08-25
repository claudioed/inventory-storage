package postgres

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/claudioed/inventory-storage/internal/domain/product"
	"github.com/claudioed/inventory-storage/internal/domain/shared"
)

// ProductClassificationRepo is a pgxpool-backed implementation of
// ports.ProductClassificationRepo — this service's durable store for the
// SKU-level master data it owns as source of truth (ADR 0009).
type ProductClassificationRepo struct {
	pool *pgxpool.Pool
}

func NewProductClassificationRepo(pool *pgxpool.Pool) *ProductClassificationRepo {
	return &ProductClassificationRepo{pool: pool}
}

func (r *ProductClassificationRepo) Save(ctx context.Context, c *product.ProductClassification) error {
	tags := c.HandlingTags()
	rawTags := make([]string, 0, len(tags))
	for _, tag := range tags {
		rawTags = append(rawTags, string(tag))
	}

	_, err := r.pool.Exec(ctx, `
		INSERT INTO product_classifications (sku, handling_tags, temperature_class)
		VALUES ($1, $2, $3)
		ON CONFLICT (sku) DO UPDATE SET handling_tags = EXCLUDED.handling_tags, temperature_class = EXCLUDED.temperature_class
	`, c.SKU().String(), rawTags, string(c.TemperatureClass()))
	return err
}

func (r *ProductClassificationRepo) FindBySKU(ctx context.Context, sku shared.SKU) (*product.ProductClassification, error) {
	var rawTags []string
	var temperatureClass string
	err := r.pool.QueryRow(ctx, `
		SELECT handling_tags, temperature_class FROM product_classifications WHERE sku = $1
	`, sku.String()).Scan(&rawTags, &temperatureClass)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	tags := make([]product.HandlingTag, 0, len(rawTags))
	for _, raw := range rawTags {
		tags = append(tags, product.HandlingTag(raw))
	}

	return product.Rehydrate(sku, tags, product.TemperatureClass(temperatureClass)), nil
}
