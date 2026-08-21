package shared

// SKU identifies a stock keeping unit — the product being stored.
type SKU string

// NewSKU validates and constructs a SKU. A SKU must not be empty: the domain
// requires an item-scan to identify what is being handled.
func NewSKU(value string) (SKU, error) {
	if value == "" {
		return "", ErrEmptySKU
	}
	return SKU(value), nil
}

func (s SKU) String() string {
	return string(s)
}
