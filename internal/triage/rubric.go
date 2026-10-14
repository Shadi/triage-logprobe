package triage

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"

	triagelogprobs "github.com/shadi/triage-logprobs"
	"github.com/shadi/triage-logprobs/internal/decision"
)

type Rubric struct {
	System   string            `json:"system"`
	Question string            `json:"question"`
	Options  []decision.Option `json:"options"`
}

func DefaultRubric() (Rubric, error) {
	return parseRubric([]byte(triagelogprobs.Rubric))
}

func LoadRubric(path string) (Rubric, error) {
	if path == "" {
		return DefaultRubric()
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return Rubric{}, err
	}
	return parseRubric(b)
}

func parseRubric(b []byte) (Rubric, error) {
	var r Rubric
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&r); err != nil {
		return Rubric{}, fmt.Errorf("rubric: %w", err)
	}
	if dec.More() {
		return Rubric{}, fmt.Errorf("rubric: unexpected data after the JSON object")
	}
	if err := r.SevQuestion().Validate(); err != nil {
		return Rubric{}, fmt.Errorf("rubric: %w", err)
	}
	return r, nil
}

func (r Rubric) SevQuestion() decision.Question {
	return decision.Question{Kind: decision.KindChoice, Instructions: r.Question, Options: r.Options}
}
