package shared

import "testing"

func TestNewQuantity_RejectsNegative(t *testing.T) {
	if _, err := NewQuantity(-1); err != ErrNegativeQuantity {
		t.Fatalf("expected ErrNegativeQuantity, got %v", err)
	}
}

func TestNewQuantity_AllowsZero(t *testing.T) {
	q, err := NewQuantity(0)
	if err != nil || q.Int() != 0 {
		t.Fatalf("expected zero quantity, got %v err=%v", q, err)
	}
}

func TestNewPositiveQuantity_RejectsZero(t *testing.T) {
	if _, err := NewPositiveQuantity(0); err != ErrZeroQuantity {
		t.Fatalf("expected ErrZeroQuantity, got %v", err)
	}
}

func TestQuantity_Sub_RejectsNegativeResult(t *testing.T) {
	q, _ := NewQuantity(2)
	other, _ := NewQuantity(5)
	if _, err := q.Sub(other); err != ErrNegativeQuantity {
		t.Fatalf("expected ErrNegativeQuantity, got %v", err)
	}
}

func TestSKU_RejectsEmpty(t *testing.T) {
	if _, err := NewSKU(""); err != ErrEmptySKU {
		t.Fatalf("expected ErrEmptySKU, got %v", err)
	}
}

func TestBinId_RejectsEmpty(t *testing.T) {
	if _, err := NewBinId(""); err != ErrEmptyBinID {
		t.Fatalf("expected ErrEmptyBinID, got %v", err)
	}
}
