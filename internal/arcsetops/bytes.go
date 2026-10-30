package arcsetops

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// ParseBytes parses a byte count with an optional unit suffix (KiB/TiB/etc.).
func ParseBytes(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("value is required")
	}
	multiplier := int64(1)
	upper := strings.ToUpper(s)
	for _, suffix := range []struct {
		name string
		mult int64
	}{
		{"PIB", 1 << 50}, {"TIB", 1 << 40}, {"GIB", 1 << 30}, {"MIB", 1 << 20}, {"KIB", 1 << 10},
		{"PB", 1 << 50}, {"TB", 1 << 40}, {"GB", 1 << 30}, {"MB", 1 << 20}, {"KB", 1 << 10},
		{"P", 1 << 50}, {"T", 1 << 40}, {"G", 1 << 30}, {"M", 1 << 20}, {"K", 1 << 10},
	} {
		if strings.HasSuffix(upper, suffix.name) {
			multiplier = suffix.mult
			s = strings.TrimSpace(s[:len(s)-len(suffix.name)])
			break
		}
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, err
	}
	if n <= 0 || n > math.MaxInt64/multiplier {
		return 0, fmt.Errorf("value out of range")
	}
	return n * multiplier, nil
}
