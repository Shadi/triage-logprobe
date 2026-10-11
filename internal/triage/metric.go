package triage

import (
	"bufio"
	"fmt"
	"io"
	"regexp"
	"strings"
)

type Metric struct {
	Name  string `json:"name"`
	Query string `json:"query"`
}

var metricName = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

func ParseMetric(s string) (Metric, error) {
	name, query, ok := strings.Cut(s, "=")
	name, query = strings.TrimSpace(name), strings.TrimSpace(query)
	if !ok || query == "" {
		return Metric{}, fmt.Errorf("metric %q: want name=promql", s)
	}
	if !metricName.MatchString(name) {
		return Metric{}, fmt.Errorf("metric name %q: want lowercase letters, digits and underscores", name)
	}
	return Metric{Name: name, Query: query}, nil
}

func ParseMetrics(r io.Reader) ([]Metric, error) {
	var out []Metric
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		m, err := ParseMetric(line)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", n, err)
		}
		out = append(out, m)
	}
	return out, sc.Err()
}

func CheckUnique(ms []Metric) error {
	seen := make(map[string]bool, len(ms))
	for _, m := range ms {
		if seen[m.Name] {
			return fmt.Errorf("duplicate metric name %q", m.Name)
		}
		seen[m.Name] = true
	}
	return nil
}
