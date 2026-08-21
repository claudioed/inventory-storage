package shared

// Quantity is a non-negative count of units. It is a value object: every
// operation that would drive a quantity negative returns an error instead of
// silently clamping, so "no negative usable" is enforced at construction and
// on every arithmetic operation.
type Quantity int

// NewQuantity validates and constructs a Quantity. Negative quantities are
// rejected outright; a zero quantity is permitted (e.g. a fully picked lot).
func NewQuantity(value int) (Quantity, error) {
	if value < 0 {
		return 0, ErrNegativeQuantity
	}
	return Quantity(value), nil
}

// NewPositiveQuantity validates and constructs a Quantity that must be
// strictly greater than zero (e.g. the quantity of a receive or reserve
// request, which can never be "reserve nothing").
func NewPositiveQuantity(value int) (Quantity, error) {
	if value <= 0 {
		return 0, ErrZeroQuantity
	}
	return Quantity(value), nil
}

// Add returns q + other.
func (q Quantity) Add(other Quantity) Quantity {
	return q + other
}

// Sub returns q - other, or an error if the result would be negative.
func (q Quantity) Sub(other Quantity) (Quantity, error) {
	result := q - other
	if result < 0 {
		return 0, ErrNegativeQuantity
	}
	return result, nil
}

// GreaterThan reports whether q > other.
func (q Quantity) GreaterThan(other Quantity) bool {
	return q > other
}

// Int returns the plain int value.
func (q Quantity) Int() int {
	return int(q)
}
