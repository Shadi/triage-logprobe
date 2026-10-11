package decision

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

type Kind string

const (
	KindChoice Kind = "choice"
	KindScore  Kind = "score"
	KindNoul   Kind = "noul"
)

const (
	NoulYes = "yes"
	NoulNo  = "no"
)

var noulOptions = []Option{
	{Key: NoulYes, Description: "Yes, the statement is true for this state"},
	{Key: NoulNo, Description: "No, the statement is false for this state"},
}

type Question struct {
	Kind         Kind
	Instructions string
	Options      []Option
}

type Answer struct {
	Kind       Kind               `json:"kind"`
	Probs      map[string]float64 `json:"probabilities"`
	Confidence float64            `json:"confidence"`
	Margin     float64            `json:"margin"`
	OptionMass float64            `json:"option_mass"`
	Missing    []string           `json:"missing,omitempty"`
	Choice     string             `json:"choice,omitempty"`
	Score      *float64           `json:"score,omitempty"`
	Yes        *float64           `json:"yes,omitempty"`
}

var ErrInvalidQuestion = errors.New("decision: invalid question")

func (q Question) Validate() error {
	if strings.TrimSpace(q.Instructions) == "" {
		return fmt.Errorf("%w: %s questions need Instructions", ErrInvalidQuestion, q.Kind)
	}
	switch q.Kind {
	case KindChoice, KindScore:
		if err := validateOptions(q.Options); err != nil {
			return fmt.Errorf("%w: %s: %w", ErrInvalidQuestion, q.Kind, err)
		}
	case KindNoul:
		if len(q.Options) > 0 {
			return fmt.Errorf("%w: noul questions take no options", ErrInvalidQuestion)
		}
	default:
		return fmt.Errorf("%w: unknown kind %q", ErrInvalidQuestion, q.Kind)
	}
	return nil
}

func (q Question) label() string {
	if q.Kind == KindNoul {
		return "Statement"
	}
	return "Question"
}

func (q Question) options() []Option {
	if q.Kind == KindNoul {
		return slices.Clone(noulOptions)
	}
	return q.Options
}
