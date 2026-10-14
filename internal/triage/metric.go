package triage

import (
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"regexp"
	"strings"

	"go.yaml.in/yaml/v3"
)

type Target struct {
	Min *float64 `json:"min,omitempty" yaml:"min"`
	Max *float64 `json:"max,omitempty" yaml:"max"`
}

type Metric struct {
	Name   string  `json:"name" yaml:"name"`
	Source string  `json:"source" yaml:"-"`
	Query  string  `json:"query" yaml:"query"`
	Target *Target `json:"target,omitempty" yaml:"target"`
}

type metricsFile struct {
	Sources []sourceGroup `yaml:"sources"`
}

type sourceGroup struct {
	Source  string   `yaml:"source"`
	Metrics []Metric `yaml:"metrics"`
}

var (
	metricName = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
	sourceName = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)
)

func LoadMetrics(path string) ([]Metric, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	ms, err := ParseMetrics(f)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return ms, nil
}

func ParseMetrics(r io.Reader) ([]Metric, error) {
	dec := yaml.NewDecoder(r)
	dec.KnownFields(true)
	var f metricsFile
	if err := dec.Decode(&f); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, errors.New("empty metrics file")
		}
		return nil, err
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, errors.New("want a single YAML document")
	}
	if len(f.Sources) == 0 {
		return nil, errors.New("no sources")
	}
	var out []Metric
	for i, g := range f.Sources {
		if !sourceName.MatchString(g.Source) {
			return nil, fmt.Errorf("sources[%d]: source %q: want lowercase letters, digits, dashes and underscores", i, g.Source)
		}
		if len(g.Metrics) == 0 {
			return nil, fmt.Errorf("sources[%d] %q: no metrics", i, g.Source)
		}
		for j, m := range g.Metrics {
			m.Source = g.Source
			m.Query = strings.TrimSpace(m.Query)
			if err := m.validate(); err != nil {
				return nil, fmt.Errorf("sources[%d].metrics[%d]: %w", i, j, err)
			}
			out = append(out, m)
		}
	}
	if err := CheckUnique(out); err != nil {
		return nil, err
	}
	return out, nil
}

func (m Metric) validate() error {
	if !metricName.MatchString(m.Name) {
		return fmt.Errorf("metric name %q: want lowercase letters, digits and underscores", m.Name)
	}
	if m.Query == "" {
		return fmt.Errorf("metric %q: empty query", m.Name)
	}
	if m.Target != nil {
		if err := m.Target.validate(); err != nil {
			return fmt.Errorf("metric %q target: %w", m.Name, err)
		}
	}
	return nil
}

func (t Target) validate() error {
	if t.Min == nil && t.Max == nil {
		return errors.New("want min, max or both")
	}
	for _, v := range []*float64{t.Min, t.Max} {
		if v != nil && (math.IsNaN(*v) || math.IsInf(*v, 0)) {
			return fmt.Errorf("%v is not a finite number", *v)
		}
	}
	if t.Min != nil && t.Max != nil && *t.Min > *t.Max {
		return fmt.Errorf("min %v is above max %v", *t.Min, *t.Max)
	}
	return nil
}

func RequireTargets(ms []Metric) error {
	for _, m := range ms {
		if m.Target == nil {
			return fmt.Errorf("metric %q has no target", m.Name)
		}
	}
	return nil
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
