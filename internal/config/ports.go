package config

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// ParsePorts parses a comma-separated list of port numbers. Values are
// deduplicated, sorted, and validated to be in the range [1, 65535].
func ParsePorts(s string) ([]int, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, fmt.Errorf("ports cannot be empty")
	}

	parts := strings.Split(s, ",")
	seen := make(map[int]struct{}, len(parts))
	ports := make([]int, 0, len(parts))

	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			return nil, fmt.Errorf("empty port in list %q", s)
		}
		v, err := strconv.Atoi(part)
		if err != nil {
			return nil, fmt.Errorf("invalid port %q: %w", part, err)
		}
		if v < 1 || v > 65535 {
			return nil, fmt.Errorf("port %d out of range [1, 65535]", v)
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		ports = append(ports, v)
	}

	sort.Ints(ports)
	return ports, nil
}

// FormatPorts joins a sorted slice of ports as a comma-separated string.
func FormatPorts(ports []int) string {
	parts := make([]string, len(ports))
	for i, p := range ports {
		parts[i] = strconv.Itoa(p)
	}
	return strings.Join(parts, ",")
}
