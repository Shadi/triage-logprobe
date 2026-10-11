package triage

import (
	"context"

	"github.com/shadi/triage-logprobe/internal/decision"
)

const SeverityID = "severity"

func Assess(ctx context.Context, e *decision.Engine, r Rubric, state string) (decision.Answer, error) {
	answers, err := e.Decide(ctx, state, map[string]decision.Question{SeverityID: r.SevQuestion()})
	if err != nil {
		return decision.Answer{}, err
	}
	return answers[SeverityID], nil
}
