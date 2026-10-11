package llm

import "context"

type TokenLogprob struct {
	Token   string
	Logprob float64
	Bytes   []byte
}

func (t TokenLogprob) Text() string {
	if len(t.Bytes) > 0 {
		return string(t.Bytes)
	}
	return t.Token
}

type LogprobSource interface {
	NextTokenLogprobs(ctx context.Context, model, rawPrompt string, topN int) ([]TokenLogprob, error)
}
