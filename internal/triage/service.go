package triage

import (
	"fmt"
	"regexp"
	"strings"
)

type Service struct {
	Name    string   `json:"name"`
	Metrics []Metric `json:"metrics"`
}

var serviceName = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

func ParseService(s string) (Service, error) {
	name, path, ok := strings.Cut(s, "=")
	name, path = strings.TrimSpace(name), strings.TrimSpace(path)
	if !ok || path == "" {
		return Service{}, fmt.Errorf("service %q: want name=path/to/metrics.yaml", s)
	}
	if !serviceName.MatchString(name) {
		return Service{}, fmt.Errorf("service name %q: want lowercase letters, digits and dashes", name)
	}
	ms, err := LoadMetrics(path)
	if err != nil {
		return Service{}, fmt.Errorf("service %q: %w", name, err)
	}
	return Service{Name: name, Metrics: ms}, nil
}

func CheckUniqueServices(ss []Service) error {
	seen := make(map[string]bool, len(ss))
	for _, s := range ss {
		if seen[s.Name] {
			return fmt.Errorf("duplicate service name %q", s.Name)
		}
		seen[s.Name] = true
	}
	return nil
}
