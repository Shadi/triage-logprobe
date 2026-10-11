package decision

import (
	"errors"
	"fmt"
	"strings"

	"github.com/shadi/triage-logprobe/internal/llm"
)

const MaxTopN = 20

type EngineOption func(*Engine)

func WithModel(m string) EngineOption { return func(e *Engine) { e.model = m } }

func WithSystem(s string) EngineOption { return func(e *Engine) { e.system = s } }

func WithTemplate(t Template) EngineOption { return func(e *Engine) { e.template = t } }

func WithFormat(f Format) EngineOption { return func(e *Engine) { e.format = f } }

func WithTopN(n int) EngineOption { return func(e *Engine) { e.topN = n } }

func WithMinOptionMass(m float64) EngineOption { return func(e *Engine) { e.minOptionMass = m } }

func WithDebias(on bool) EngineOption { return func(e *Engine) { e.debias = on } }

var ErrInvalidConfig = errors.New("decision: invalid engine config")

func New(src llm.LogprobSource, opts ...EngineOption) (*Engine, error) {
	e := &Engine{
		source:        src,
		model:         DefaultModel,
		template:      QwenNoThink,
		format:        DefaultFormat,
		system:        DefaultSystem,
		topN:          DefaultTopN,
		minOptionMass: DefaultMinOptionMass,
	}
	for _, o := range opts {
		o(e)
	}
	if err := e.validate(); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidConfig, err)
	}
	return e, nil
}

func (e *Engine) validate() error {
	switch {
	case e.source == nil:
		return errors.New("source is nil")
	case strings.TrimSpace(e.model) == "":
		return errors.New("model is empty")
	case e.template.TurnStart == "" || e.template.TurnEnd == "":
		return errors.New("template is incomplete")
	case e.format.Instruction == "" || e.format.Prefill == "":
		return errors.New("format is incomplete")
	case e.topN < MaxOptions || e.topN > MaxTopN:
		return fmt.Errorf("topN must be between %d and %d, got %d", MaxOptions, MaxTopN, e.topN)
	case !(e.minOptionMass > 0 && e.minOptionMass <= 1):
		return fmt.Errorf("min option mass must be in (0, 1], got %v", e.minOptionMass)
	}
	return checkReserved(e.template, "system", e.system)
}
