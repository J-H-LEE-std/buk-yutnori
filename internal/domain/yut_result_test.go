package domain

import (
	"errors"
	"testing"
)

func TestYutResultOrdinaryMovementSpaces(t *testing.T) {
	tests := []struct {
		result YutResult
		want   int
	}{
		{result: YutDo, want: 1},
		{result: YutGae, want: 2},
		{result: YutGeol, want: 3},
		{result: YutYut, want: 4},
		{result: YutMo, want: 5},
	}

	for _, test := range tests {
		t.Run(test.result.String(), func(t *testing.T) {
			got, err := test.result.OrdinaryMovementSpaces()
			if err != nil {
				t.Fatalf("OrdinaryMovementSpaces() error = %v", err)
			}
			if got != test.want {
				t.Fatalf("OrdinaryMovementSpaces() = %d, want %d", got, test.want)
			}
		})
	}
}

func TestYutResultOrdinaryMovementSpacesRejectsSpecialAndInvalidResults(t *testing.T) {
	for _, result := range []YutResult{YutBackdo, YutBuk} {
		t.Run(result.String(), func(t *testing.T) {
			spaces, err := result.OrdinaryMovementSpaces()
			if !errors.Is(err, ErrNotOrdinaryYutResult) {
				t.Fatalf("OrdinaryMovementSpaces() error = %v, want ErrNotOrdinaryYutResult", err)
			}
			if spaces != 0 {
				t.Fatalf("OrdinaryMovementSpaces() spaces = %d, want 0", spaces)
			}
		})
	}

	invalid := YutResult("unknown")
	spaces, err := invalid.OrdinaryMovementSpaces()
	if !errors.Is(err, ErrInvalidEnumValue) {
		t.Fatalf("OrdinaryMovementSpaces() invalid error = %v, want ErrInvalidEnumValue", err)
	}
	if spaces != 0 {
		t.Fatalf("OrdinaryMovementSpaces() invalid spaces = %d, want 0", spaces)
	}
}
