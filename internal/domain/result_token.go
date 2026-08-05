package domain

// ResultTokenID is the stable identifier of one generated throw result.
type ResultTokenID string

// Validate reports whether id satisfies the shared non-empty ID constraint.
func (id ResultTokenID) Validate() error {
	return validateID("ResultTokenID", string(id))
}

// String returns the identifier unchanged.
func (id ResultTokenID) String() string {
	return string(id)
}

// MarshalJSON encodes id as a validated JSON string.
func (id ResultTokenID) MarshalJSON() ([]byte, error) {
	return marshalValidatedString(id, ResultTokenID.Validate)
}

// UnmarshalJSON decodes and validates a JSON string.
func (id *ResultTokenID) UnmarshalJSON(data []byte) error {
	value, err := unmarshalValidatedString(data, ResultTokenID.Validate)
	if err != nil {
		return err
	}
	*id = value
	return nil
}

// ResultOrigin identifies why a result token was generated.
type ResultOrigin string

const (
	ResultOriginInitialThrow ResultOrigin = "initial_throw"
	ResultOriginYutExtra     ResultOrigin = "yut_extra"
	ResultOriginMoExtra      ResultOrigin = "mo_extra"
	ResultOriginCaptureExtra ResultOrigin = "capture_extra"
)

// Validate reports whether origin is canonical.
func (origin ResultOrigin) Validate() error {
	switch origin {
	case ResultOriginInitialThrow,
		ResultOriginYutExtra,
		ResultOriginMoExtra,
		ResultOriginCaptureExtra:
		return nil
	default:
		return invalidEnum("ResultOrigin", string(origin))
	}
}

// String returns the canonical JSON string value.
func (origin ResultOrigin) String() string {
	return string(origin)
}

// MarshalJSON encodes origin as a validated JSON string.
func (origin ResultOrigin) MarshalJSON() ([]byte, error) {
	return marshalValidatedString(origin, ResultOrigin.Validate)
}

// UnmarshalJSON decodes and validates a JSON string.
func (origin *ResultOrigin) UnmarshalJSON(data []byte) error {
	value, err := unmarshalValidatedString(data, ResultOrigin.Validate)
	if err != nil {
		return err
	}
	*origin = value
	return nil
}
