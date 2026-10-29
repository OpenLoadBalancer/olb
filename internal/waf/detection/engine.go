package detection

import (
	"path"
	"strings"
)

// Threshold defines score thresholds for blocking and logging.
type Threshold struct {
	Block int // total score >= this → block (default: 50)
	Log   int // total score >= this → log (default: 25)
}

// DefaultThreshold returns the default detection thresholds.
func DefaultThreshold() Threshold {
	return Threshold{Block: 50, Log: 25}
}

// Exclusion defines a path+detector combination to exclude from detection.
type Exclusion struct {
	PathPattern string
	Detectors   []string
	// Condition is "always" (default) or "whitelist": apply only to requests
	// from whitelisted clients.
	Condition string
}

// Result holds the output of the detection engine.
type Result struct {
	Findings   []Finding
	TotalScore int
	Blocked    bool
	Logged     bool
}

// Engine runs all registered detectors and accumulates scores.
type Engine struct {
	detectors   []Detector
	threshold   Threshold
	exclusions  []Exclusion
	multipliers map[string]float64
}

// Config configures the detection engine.
type Config struct {
	Threshold   Threshold
	Exclusions  []Exclusion
	Multipliers map[string]float64
}

// NewEngine creates a new detection engine.
func NewEngine(cfg Config) *Engine {
	if cfg.Threshold.Block == 0 {
		cfg.Threshold = DefaultThreshold()
	}
	if cfg.Multipliers == nil {
		cfg.Multipliers = make(map[string]float64)
	}
	return &Engine{
		threshold:   cfg.Threshold,
		exclusions:  cfg.Exclusions,
		multipliers: cfg.Multipliers,
	}
}

// Register adds a detector to the engine.
func (e *Engine) Register(d Detector) {
	e.detectors = append(e.detectors, d)
}

// Detect runs all detectors against the request context and returns the result.
func (e *Engine) Detect(ctx *RequestContext) Result {
	var result Result

	for _, d := range e.detectors {
		if e.isExcluded(ctx, d.Name()) {
			continue
		}

		findings := d.Detect(ctx)
		for _, f := range findings {
			if mult, ok := e.multipliers[d.Name()]; ok && mult != 0 {
				f.Score = int(float64(f.Score) * mult)
			}
			result.Findings = append(result.Findings, f)
			result.TotalScore += f.Score
		}
	}

	result.Blocked = result.TotalScore >= e.threshold.Block
	result.Logged = result.TotalScore >= e.threshold.Log

	return result
}

func (e *Engine) isExcluded(ctx *RequestContext, detectorName string) bool {
	reqPath := ctx.Path
	for _, ex := range e.exclusions {
		if strings.EqualFold(ex.Condition, "whitelist") && !ctx.IsWhitelisted {
			continue
		}
		for _, d := range ex.Detectors {
			if !strings.EqualFold(d, detectorName) {
				continue
			}
			if matched, _ := path.Match(ex.PathPattern, reqPath); matched {
				return true
			}
		}
	}
	return false
}
