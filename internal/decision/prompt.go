package decision

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

const (
	MinOptions = 2
	MaxOptions = 9

	DefaultChoiceInstructions = "Which option best fits the state?"
)

type Format struct {
	Instruction string
	Prefill     string
}

var DefaultFormat = Format{
	Instruction: "Select one option. Answer only with its bracketed numeric identifier.",
	Prefill:     "Best answer: [",
}

type Option struct {
	Key         string `json:"key"`
	Description string `json:"description"`
}

type Template struct {
	Name            string
	TurnStart       string
	TurnEnd         string
	AssistantPrefix string
}

var QwenNoThink = Template{
	Name:            "qwen-chatml-nothink",
	TurnStart:       "<|im_start|>",
	TurnEnd:         "<|im_end|>\n",
	AssistantPrefix: "<think>\n\n</think>\n\n",
}

func (t Template) Reserved() []string {
	r := []string{"<|", "<think>", "</think>"}
	for _, s := range []string{t.TurnStart, strings.TrimSpace(t.TurnEnd)} {
		if s != "" {
			r = append(r, s)
		}
	}
	return r
}

func (t Template) Render(system, user, prefill string) string {
	var sb strings.Builder
	if system != "" {
		sb.WriteString(t.TurnStart + "system\n" + system + t.TurnEnd)
	}
	sb.WriteString(t.TurnStart + "user\n" + user + t.TurnEnd)
	sb.WriteString(t.TurnStart + "assistant\n" + t.AssistantPrefix + prefill)
	return sb.String()
}

func BuildPrompt(tpl Template, f Format, system, state string, q Question) (string, error) {
	if err := q.Validate(); err != nil {
		return "", err
	}
	return buildPrompt(tpl, f, system, state, q, q.options())
}

func buildPrompt(tpl Template, f Format, system, state string, q Question, opts []Option) (string, error) {
	if strings.TrimSpace(state) == "" {
		return "", errors.New("decision: state is empty")
	}
	if err := checkReserved(tpl, "state", state); err != nil {
		return "", err
	}
	if err := checkReserved(tpl, "system", system); err != nil {
		return "", err
	}
	if err := checkReserved(tpl, "instructions", q.Instructions); err != nil {
		return "", err
	}
	for _, o := range opts {
		if err := checkReserved(tpl, "option "+strconv.Quote(o.Key), o.Description); err != nil {
			return "", err
		}
	}

	var user strings.Builder
	user.WriteString("State: " + strings.TrimSpace(state) + "\n")
	user.WriteString(q.label() + ": " + oneLine(q.Instructions) + "\n")
	user.WriteString("Options:\n")
	for i, o := range opts {
		user.WriteString("[" + strconv.Itoa(i+1) + "] " + oneLine(o.Description) + "\n")
	}
	user.WriteString(f.Instruction)

	return tpl.Render(strings.TrimSpace(system), user.String(), f.Prefill), nil
}

func validateOptions(opts []Option) error {
	if len(opts) < MinOptions || len(opts) > MaxOptions {
		return fmt.Errorf("decision: need %d to %d options, got %d", MinOptions, MaxOptions, len(opts))
	}
	seen := make(map[string]bool, len(opts))
	for i, o := range opts {
		if o.Key == "" {
			return fmt.Errorf("decision: option %d has an empty key", i+1)
		}
		if seen[o.Key] {
			return fmt.Errorf("decision: duplicate option key %q", o.Key)
		}
		seen[o.Key] = true
		if strings.TrimSpace(o.Description) == "" {
			return fmt.Errorf("decision: option %q has an empty description", o.Key)
		}
	}
	return nil
}

var ErrReservedToken = errors.New("decision: input contains a reserved template token")

func checkReserved(tpl Template, field, s string) error {
	for _, r := range tpl.Reserved() {
		if strings.Contains(s, r) {
			return fmt.Errorf("%w: %s contains %q", ErrReservedToken, field, r)
		}
	}
	return nil
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
