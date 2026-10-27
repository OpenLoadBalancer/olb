package utils

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// ParseDuration parses a duration string with support for human-readable formats.
// Supports: ns, us, ms, s, m, h, d (day), w (week)
// Also supports combined formats like "1h30m", "2d12h"
func ParseDuration(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, nil
	}

	// Try standard parsing first
	d, err := time.ParseDuration(s)
	if err == nil {
		return d, nil
	}

	// Try custom parsing for days, weeks
	return parseCustomDuration(s)
}

// parseCustomDuration parses duration with d (day) and w (week) support.
func parseCustomDuration(s string) (time.Duration, error) {
	var total time.Duration
	var numStr strings.Builder

	// Index loop, not range: the "ms"/"us" cases advance i past the 's'.
	for i := 0; i < len(s); i++ {
		c := s[i]

		if unicode.IsDigit(rune(c)) || c == '.' {
			numStr.WriteByte(c)
			continue
		}

		if numStr.Len() == 0 {
			return 0, strconv.ErrSyntax
		}

		num, err := strconv.ParseFloat(numStr.String(), 64)
		if err != nil {
			return 0, err
		}
		numStr.Reset()

		var unitDur time.Duration
		switch c {
		case 'w', 'W':
			unitDur = 7 * 24 * time.Hour
		case 'd', 'D':
			unitDur = 24 * time.Hour
		case 'h', 'H':
			unitDur = time.Hour
		case 'm', 'M':
			// Check if next char is 's' or 'S' for milliseconds
			if i+1 < len(s) && (s[i+1] == 's' || s[i+1] == 'S') {
				unitDur = time.Millisecond
				i++ // skip 's'
			} else {
				unitDur = time.Minute
			}
		case 's', 'S':
			unitDur = time.Second
		case 'u', 'U':
			// Check for us (microseconds)
			if i+1 < len(s) && s[i+1] == 's' {
				unitDur = time.Microsecond
				i++
			}
		default:
			return 0, strconv.ErrSyntax
		}

		scaled := num * float64(unitDur)
		if scaled > float64(math.MaxInt64) || scaled < float64(math.MinInt64) {
			return 0, fmt.Errorf("duration overflow: %g%s exceeds maximum", num, string(c))
		}
		total += time.Duration(scaled)
	}

	if numStr.Len() > 0 {
		return 0, strconv.ErrSyntax
	}

	return total, nil
}

// MustParseDuration parses a duration string and panics on error.
func MustParseDuration(s string) time.Duration {
	d, err := ParseDuration(s)
	if err != nil {
		panic(err)
	}
	return d
}

// FormatDuration formats a duration in a human-readable format.
// Example: 3661s -> "1h1m1s", 86400s -> "1d"
func FormatDuration(d time.Duration) string {
	if d == 0 {
		return "0s"
	}

	var parts []string

	// Handle negative durations
	if d < 0 {
		parts = append(parts, "-")
		d = -d
	}

	// Weeks
	weeks := d / (7 * 24 * time.Hour)
	if weeks > 0 {
		parts = append(parts, strconv.FormatInt(int64(weeks), 10)+"w")
		d -= weeks * 7 * 24 * time.Hour
	}

	// Days
	days := d / (24 * time.Hour)
	if days > 0 {
		parts = append(parts, strconv.FormatInt(int64(days), 10)+"d")
		d -= days * 24 * time.Hour
	}

	// Hours
	hours := d / time.Hour
	if hours > 0 {
		parts = append(parts, strconv.FormatInt(int64(hours), 10)+"h")
		d -= hours * time.Hour
	}

	// Minutes
	minutes := d / time.Minute
	if minutes > 0 {
		parts = append(parts, strconv.FormatInt(int64(minutes), 10)+"m")
		d -= minutes * time.Minute
	}

	// Seconds and sub-seconds
	if d > 0 || len(parts) == 0 {
		seconds := float64(d) / float64(time.Second)
		if seconds == float64(int64(seconds)) {
			// Whole seconds
			parts = append(parts, strconv.FormatInt(int64(seconds), 10)+"s")
		} else {
			// Include milliseconds
			parts = append(parts, strconv.FormatFloat(seconds, 'f', 3, 64)+"s")
		}
	}

	return strings.Join(parts, "")
}

// ParseByteSize parses a human-readable byte size string.
// Supports: B, KB, MB, GB, TB, PB (case-insensitive)
// Also supports K, M, G, T, P, KiB, MiB, etc.
// Returns bytes as int64.
func ParseByteSize(s string) (int64, error) {
	s = strings.TrimSpace(strings.ToUpper(s))
	if s == "" {
		return 0, nil
	}

	// Find where the number ends
	var numStr strings.Builder
	i := 0
	for ; i < len(s); i++ {
		c := s[i]
		if (c >= '0' && c <= '9') || c == '.' {
			numStr.WriteByte(c)
		} else {
			break
		}
	}

	if numStr.Len() == 0 {
		return 0, strconv.ErrSyntax
	}

	num, err := strconv.ParseFloat(numStr.String(), 64)
	if err != nil {
		return 0, err
	}

	// Parse unit
	unit := s[i:]
	unit = strings.TrimSpace(unit)

	// Check for binary units (IEC)
	isBinary := strings.Contains(unit, "IB")
	unit = strings.TrimSuffix(unit, "IB")
	unit = strings.TrimSuffix(unit, "B")

	var multiplier float64
	switch unit {
	case "", "B":
		multiplier = 1
	case "K":
		if isBinary {
			multiplier = 1024
		} else {
			multiplier = 1000
		}
	case "M":
		if isBinary {
			multiplier = 1024 * 1024
		} else {
			multiplier = 1000 * 1000
		}
	case "G":
		if isBinary {
			multiplier = 1024 * 1024 * 1024
		} else {
			multiplier = 1000 * 1000 * 1000
		}
	case "T":
		if isBinary {
			multiplier = 1024.0 * 1024 * 1024 * 1024
		} else {
			multiplier = 1000.0 * 1000 * 1000 * 1000
		}
	case "P":
		if isBinary {
			multiplier = 1024.0 * 1024 * 1024 * 1024 * 1024
		} else {
			multiplier = 1000.0 * 1000 * 1000 * 1000 * 1000
		}
	default:
		return 0, strconv.ErrSyntax
	}

	result := num * multiplier
	if result > float64(math.MaxInt64) || result < 0 {
		return 0, fmt.Errorf("byte size overflow: %q exceeds maximum int64 value", s)
	}
	return int64(result), nil
}

// MustParseByteSize parses a byte size string and panics on error.
func MustParseByteSize(s string) int64 {
	n, err := ParseByteSize(s)
	if err != nil {
		panic(err)
	}
	return n
}

// FormatByteSize formats bytes as a human-readable string.
// Uses binary units (KiB, MiB, etc.) for values >= 1024.
func FormatByteSize(bytes int64) string {
	if bytes < 0 {
		return "-" + FormatByteSize(-bytes)
	}
	if bytes == 0 {
		return "0 B"
	}

	const (
		KB = 1024
		MB = 1024 * KB
		GB = 1024 * MB
		TB = 1024 * GB
		PB = 1024 * TB
	)

	switch {
	case bytes >= PB:
		return formatSize(float64(bytes)/float64(PB), "PiB")
	case bytes >= TB:
		return formatSize(float64(bytes)/float64(TB), "TiB")
	case bytes >= GB:
		return formatSize(float64(bytes)/float64(GB), "GiB")
	case bytes >= MB:
		return formatSize(float64(bytes)/float64(MB), "MiB")
	case bytes >= KB:
		return formatSize(float64(bytes)/float64(KB), "KiB")
	default:
		return strconv.FormatInt(bytes, 10) + " B"
	}
}

// formatSize formats a float with appropriate precision.
func formatSize(size float64, unit string) string {
	if size == float64(int64(size)) {
		return strconv.FormatInt(int64(size), 10) + " " + unit
	}
	return strconv.FormatFloat(size, 'f', 2, 64) + " " + unit
}

// TruncateDuration truncates a duration to the given precision.
func TruncateDuration(d time.Duration, precision time.Duration) time.Duration {
	if precision <= 0 {
		return d
	}
	return d / precision * precision
}

// RoundDuration rounds a duration to the given precision.
func RoundDuration(d time.Duration, precision time.Duration) time.Duration {
	if precision <= 0 {
		return d
	}
	half := precision / 2
	return (d + half) / precision * precision
}
