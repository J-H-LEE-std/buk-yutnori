package domain

import "fmt"

// OrdinaryMovementSpaces returns the canonical positive forward distance for
// an ordinary Yut result. Backdo and Buk require their dedicated movement
// rules and are rejected instead of being represented as numeric distances.
func (result YutResult) OrdinaryMovementSpaces() (int, error) {
	switch result {
	case YutDo:
		return 1, nil
	case YutGae:
		return 2, nil
	case YutGeol:
		return 3, nil
	case YutYut:
		return 4, nil
	case YutMo:
		return 5, nil
	case YutBackdo, YutBuk:
		return 0, fmt.Errorf("%w: %q", ErrNotOrdinaryYutResult, result)
	default:
		return 0, invalidEnum("YutResult", string(result))
	}
}
