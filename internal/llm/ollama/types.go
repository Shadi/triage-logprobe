package ollama

type generateRequest struct {
	Model       string         `json:"model"`
	Prompt      string         `json:"prompt"`
	Raw         bool           `json:"raw"`
	Stream      bool           `json:"stream"`
	Logprobs    bool           `json:"logprobs"`
	TopLogprobs int            `json:"top_logprobs"`
	KeepAlive   string         `json:"keep_alive,omitempty"`
	Options     requestOptions `json:"options"`
}

type requestOptions struct {
	NumPredict  int     `json:"num_predict"`
	Temperature float64 `json:"temperature"`
}

type generateResponse struct {
	Error    string             `json:"error"`
	Logprobs []tokenLogprobWire `json:"logprobs"`
}

type tokenLogprobWire struct {
	Token       string             `json:"token"`
	Logprob     float64            `json:"logprob"`
	Bytes       []int              `json:"bytes"`
	TopLogprobs []tokenLogprobWire `json:"top_logprobs"`
}
