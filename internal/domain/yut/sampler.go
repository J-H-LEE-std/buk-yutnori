// Package yut produces canonical Yutnori throw results.
//
// Randomness is supplied by the server through Sampler. The package does not
// use global random state and does not contain queue or movement rules.
package yut

import (
	"errors"
	"fmt"
	"math/rand/v2"
	"reflect"
	"sync"

	"buk-yutnori/internal/domain"
)

const canonicalTotalWeight uint64 = 9999

const (
	gaeWeight    uint64 = 3389
	geolWeight   uint64 = 3549
	yutWeight    uint64 = 1394
	moWeight     uint64 = 229
	backdoWeight uint64 = 359
	bukWeight    uint64 = 359
)

var canonicalResultOrder = [...]domain.YutResult{
	domain.YutDo,
	domain.YutGae,
	domain.YutGeol,
	domain.YutYut,
	domain.YutMo,
	domain.YutBackdo,
	domain.YutBuk,
}

// Mode selects the canonical probability table for enabled special results.
type Mode struct {
	BackdoEnabled  bool
	BukModeEnabled bool
}

// BoundedSource supplies a uniform integer in the half-open interval [0, limit).
// math/rand/v2.Rand satisfies this interface.
type BoundedSource interface {
	Uint64N(limit uint64) uint64
}

var (
	// ErrNilRandomSource identifies a sampler without a server-owned source.
	ErrNilRandomSource = errors.New("nil Yut random source")

	// ErrRandomSourceOutOfRange identifies a source that violated BoundedSource.
	ErrRandomSourceOutOfRange = errors.New("Yut random source returned an out-of-range value")
)

// Sampler draws results from canonical normalized weights.
//
// A sampler serializes access to its source, so one server-owned sampler can be
// shared safely. Results from concurrent callers have no defined ordering.
type Sampler struct {
	mutex  sync.Mutex
	source BoundedSource
}

// NewSampler constructs a sampler with a server-owned random source.
func NewSampler(source BoundedSource) (*Sampler, error) {
	if isNilSource(source) {
		return nil, ErrNilRandomSource
	}
	return &Sampler{source: source}, nil
}

func isNilSource(source BoundedSource) bool {
	if source == nil {
		return true
	}
	value := reflect.ValueOf(source)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

// NewSeededSampler constructs a deterministic sampler for tests.
func NewSeededSampler(seed1, seed2 uint64) *Sampler {
	source := rand.New(rand.NewPCG(seed1, seed2))
	return &Sampler{source: source}
}

// Throw returns one canonical result for mode.
func (sampler *Sampler) Throw(mode Mode) (domain.YutResult, error) {
	if sampler == nil || sampler.source == nil {
		return "", ErrNilRandomSource
	}

	sampler.mutex.Lock()
	ticket := sampler.source.Uint64N(canonicalTotalWeight)
	sampler.mutex.Unlock()
	if ticket >= canonicalTotalWeight {
		return "", fmt.Errorf(
			"%w: got %d for limit %d",
			ErrRandomSourceOutOfRange,
			ticket,
			canonicalTotalWeight,
		)
	}

	weights := canonicalWeights(mode)
	var upperBound uint64
	for _, result := range canonicalResultOrder {
		upperBound += weights[result]
		if ticket < upperBound {
			return result, nil
		}
	}

	return "", fmt.Errorf(
		"%w: no result for ticket %d",
		ErrRandomSourceOutOfRange,
		ticket,
	)
}

func canonicalWeights(mode Mode) map[domain.YutResult]uint64 {
	doWeight := uint64(1438)
	if mode.BackdoEnabled || mode.BukModeEnabled {
		doWeight = 1079
	}
	if mode.BackdoEnabled && mode.BukModeEnabled {
		doWeight = 720
	}

	weights := map[domain.YutResult]uint64{
		domain.YutDo:     doWeight,
		domain.YutGae:    gaeWeight,
		domain.YutGeol:   geolWeight,
		domain.YutYut:    yutWeight,
		domain.YutMo:     moWeight,
		domain.YutBackdo: 0,
		domain.YutBuk:    0,
	}
	if mode.BackdoEnabled {
		weights[domain.YutBackdo] = backdoWeight
	}
	if mode.BukModeEnabled {
		weights[domain.YutBuk] = bukWeight
	}
	return weights
}
