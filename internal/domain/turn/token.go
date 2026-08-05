// Package turn contains pure result queue and turn phase domain rules.
package turn

import (
	"errors"
	"fmt"

	"buk-yutnori/internal/domain"
)

// ErrInvalidResultToken identifies a token with a non-canonical field.
var ErrInvalidResultToken = errors.New("invalid result token")

// ResultToken is one stable generated result waiting to be resolved.
type ResultToken struct {
	ID                  domain.ResultTokenID `json:"token_id" yaml:"token_id"`
	Result              domain.YutResult     `json:"result" yaml:"result"`
	Origin              domain.ResultOrigin  `json:"origin" yaml:"origin"`
	GeneratedByPlayerID domain.PlayerID      `json:"generated_by_player_id" yaml:"generated_by_player_id"`
}

// Validate reports whether every token field satisfies the canonical contract.
func (token ResultToken) Validate() error {
	if err := token.ID.Validate(); err != nil {
		return fmt.Errorf("%w: token_id: %w", ErrInvalidResultToken, err)
	}
	if err := token.Result.Validate(); err != nil {
		return fmt.Errorf("%w: result: %w", ErrInvalidResultToken, err)
	}
	if err := token.Origin.Validate(); err != nil {
		return fmt.Errorf("%w: origin: %w", ErrInvalidResultToken, err)
	}
	if err := token.GeneratedByPlayerID.Validate(); err != nil {
		return fmt.Errorf("%w: generated_by_player_id: %w", ErrInvalidResultToken, err)
	}
	return nil
}
