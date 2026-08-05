package yut

import (
	"errors"
	"reflect"
	"sync"
	"testing"

	"buk-yutnori/internal/domain"
)

func TestEveryCanonicalTicketProducesExactWeights(t *testing.T) {
	tests := []struct {
		name string
		mode Mode
		want map[domain.YutResult]uint64
	}{
		{
			name: "no backdo or Buk",
			mode: Mode{},
			want: resultWeights(1438, 0, 0),
		},
		{
			name: "backdo only",
			mode: Mode{BackdoEnabled: true},
			want: resultWeights(1079, 359, 0),
		},
		{
			name: "Buk only",
			mode: Mode{BukModeEnabled: true},
			want: resultWeights(1079, 0, 359),
		},
		{
			name: "backdo and Buk",
			mode: Mode{BackdoEnabled: true, BukModeEnabled: true},
			want: resultWeights(720, 359, 359),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			source := &sequentialSource{}
			sampler, err := NewSampler(source)
			if err != nil {
				t.Fatalf("NewSampler() error = %v", err)
			}

			got := make(map[domain.YutResult]uint64)
			for range canonicalTotalWeight {
				result, err := sampler.Throw(test.mode)
				if err != nil {
					t.Fatalf("Throw() error = %v", err)
				}
				got[result]++
			}

			if source.lastLimit != canonicalTotalWeight {
				t.Fatalf("Uint64N limit = %d, want %d", source.lastLimit, canonicalTotalWeight)
			}
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("result counts = %v, want %v", got, test.want)
			}
		})
	}
}

func TestSeededSamplerReproducesResultSequence(t *testing.T) {
	first := NewSeededSampler(0x12345678, 0x9abcdef0)
	second := NewSeededSampler(0x12345678, 0x9abcdef0)
	modes := []Mode{
		{},
		{BackdoEnabled: true},
		{BukModeEnabled: true},
		{BackdoEnabled: true, BukModeEnabled: true},
	}

	for index := range 256 {
		mode := modes[index%len(modes)]
		got, err := first.Throw(mode)
		if err != nil {
			t.Fatalf("first.Throw(%d) error = %v", index, err)
		}
		want, err := second.Throw(mode)
		if err != nil {
			t.Fatalf("second.Throw(%d) error = %v", index, err)
		}
		if got != want {
			t.Fatalf("result %d = %q, want %q", index, got, want)
		}
	}
}

func TestSamplerRejectsInvalidRandomSources(t *testing.T) {
	if _, err := NewSampler(nil); !errors.Is(err, ErrNilRandomSource) {
		t.Fatalf("NewSampler(nil) error = %v, want ErrNilRandomSource", err)
	}
	var typedNil *sequentialSource
	if _, err := NewSampler(typedNil); !errors.Is(err, ErrNilRandomSource) {
		t.Fatalf("NewSampler(typed nil) error = %v, want ErrNilRandomSource", err)
	}

	sampler, err := NewSampler(outOfRangeSource{})
	if err != nil {
		t.Fatalf("NewSampler(outOfRangeSource) error = %v", err)
	}
	if _, err := sampler.Throw(Mode{}); !errors.Is(err, ErrRandomSourceOutOfRange) {
		t.Fatalf("Throw() error = %v, want ErrRandomSourceOutOfRange", err)
	}

	var zero *Sampler
	if _, err := zero.Throw(Mode{}); !errors.Is(err, ErrNilRandomSource) {
		t.Fatalf("nil Sampler.Throw() error = %v, want ErrNilRandomSource", err)
	}
}

func TestSamplerSerializesConcurrentSourceAccess(t *testing.T) {
	const throws = 1000
	source := &sequentialSource{}
	sampler, err := NewSampler(source)
	if err != nil {
		t.Fatalf("NewSampler() error = %v", err)
	}

	errors := make(chan error, throws)
	var wait sync.WaitGroup
	for range throws {
		wait.Add(1)
		go func() {
			defer wait.Done()
			_, err := sampler.Throw(Mode{BackdoEnabled: true, BukModeEnabled: true})
			errors <- err
		}()
	}
	wait.Wait()
	close(errors)

	for err := range errors {
		if err != nil {
			t.Fatalf("Throw() error = %v", err)
		}
	}
	if source.next != throws {
		t.Fatalf("source calls = %d, want %d", source.next, throws)
	}
}

func resultWeights(do, backdo, buk uint64) map[domain.YutResult]uint64 {
	weights := map[domain.YutResult]uint64{
		domain.YutDo:   do,
		domain.YutGae:  3389,
		domain.YutGeol: 3549,
		domain.YutYut:  1394,
		domain.YutMo:   229,
	}
	if backdo > 0 {
		weights[domain.YutBackdo] = backdo
	}
	if buk > 0 {
		weights[domain.YutBuk] = buk
	}
	return weights
}

type sequentialSource struct {
	next      uint64
	lastLimit uint64
}

func (source *sequentialSource) Uint64N(limit uint64) uint64 {
	source.lastLimit = limit
	value := source.next % limit
	source.next++
	return value
}

type outOfRangeSource struct{}

func (outOfRangeSource) Uint64N(limit uint64) uint64 {
	return limit
}
