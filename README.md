# triage-logprobe

Jev-style prototype of a triage decision service that reads metrics listed in [echo.txt](echo.txt) from prometheus and use a self-hosted small qwen model to classify the severity.

Incident triage from Prometheus metrics. It classifies a service as **SEV1 / SEV2 / SEV3 / HEALTHY** with a probability for each, using a small local Qwen model as a Jev-style decision model.

This is a PoC CLI, I might add later a webhook functionality so something like alertmanager or another service can contact the service directly using API calls.

## Requirements

- Go 1.27+
- [Ollama](https://ollama.com) with logprobs support (0.35+)
- The model `qwen3.5:4b` (about 3.4 GB, fits on an 8 GB+ GPU)
- A reachable Prometheus HTTP API

## Start the model

```sh
ollama serve
ollama pull qwen3.5:4b
ollama run qwen3.5:4b "hi"
```

Ollama listens on `http://localhost:11434` by default. Use `-ollama` for another address.

## Run

```sh
go run ./cmd/triage-logprobe \
  -prom http://prometheus:9090 \
  -metrics-file echo.txt \
  -show-state
```

Output:

```
HEALTHY  sev1 0.04 | sev2 0.02 | sev3 0.04 | healthy 0.90   confidence 0.69  margin 0.86  option_mass 1.00
```

### Metrics file

One metric per line, `name=PROMQL`. Lines starting with `#` are comments. Names ending in `_ratio` are shown as percentages. See `echo.txt`.

```
request_rate_rps=sum(rate(istio_requests_total{destination_service_name="checkout"}[2m]))
error_5xx_ratio=(sum(rate(istio_requests_total{destination_service_name="checkout",response_code=~"5.."}[2m])) or vector(0)) / sum(rate(istio_requests_total{destination_service_name="checkout"}[2m]))
```

### Flags

| Flag                                    | Default                                 |                                                        |
| --------------------------------------- | --------------------------------------- | ------------------------------------------------------ |
| `-prom`                                 | (required)                              | Prometheus base URL                                    |
| `-metrics-file` / `-metric name=PROMQL` |                                         | Metrics to evaluate. `-metric` can be repeated         |
| `-window`                               | `5m`                                    | How far back the incident may reach                    |
| `-baseline`                             | `30m`                                   | The "normal" period before the window (median)         |
| `-at`                                   | now                                     | Evaluate a past moment (RFC3339)                       |
| `-model` / `-ollama`                    | `qwen3.5:4b` / `http://localhost:11434` | Model and Ollama address                               |
| `-rubric`                               | built-in                                | JSON file with the system prompt, question and options |
| `-debias`                               | `true`                                  | Ask in both option orders                              |
| `-show-state` / `-json`                 |                                         | Print the model input / full JSON result               |

## Tests

```sh
go test ./...                    # unit tests, no services needed
LOGPROBE_IT=1 go test ./...      # also run integration tests against a local Ollama
```
