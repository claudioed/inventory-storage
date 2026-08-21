package shared

// BinId identifies a coded slot in the warehouse. Under chaotic storage any
// SKU may occupy any free bin; the bin is the unit of location.
type BinId string

// NewBinId validates and constructs a BinId. A BinId must not be empty: the
// domain requires a location-scan to know where an item physically is.
func NewBinId(value string) (BinId, error) {
	if value == "" {
		return "", ErrEmptyBinID
	}
	return BinId(value), nil
}

func (b BinId) String() string {
	return string(b)
}
