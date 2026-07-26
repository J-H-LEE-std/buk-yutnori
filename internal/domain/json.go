package domain

import "encoding/json"

func marshalValidatedString[T ~string](value T, validate func(T) error) ([]byte, error) {
	if err := validate(value); err != nil {
		return nil, err
	}
	return json.Marshal(string(value))
}

func unmarshalValidatedString[T ~string](data []byte, validate func(T) error) (T, error) {
	var zero T
	var raw string
	if err := json.Unmarshal(data, &raw); err != nil {
		return zero, err
	}

	value := T(raw)
	if err := validate(value); err != nil {
		return zero, err
	}
	return value, nil
}
