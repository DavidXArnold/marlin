// Package doctor implements health checks for the marlin environment.
package doctor

import "context"

// Level indicates the severity of a check result.
type Level string

const (
	LevelPass Level = "PASS"
	LevelWarn Level = "WARN"
	LevelFail Level = "FAIL"
)

// Result is the outcome of running a single check.
type Result struct {
	ID     string
	Level  Level
	Detail string // short detail shown after level on the same line
	Hint   string // shown indented below on WARN/FAIL
	CanFix bool
}

// Check is a single diagnostic check.
type Check interface {
	ID() string
	Run(ctx context.Context) Result
	Fix(ctx context.Context) error // only called when CanFix && --fix
}

// funcCheck wraps a plain function as a Check with no Fix capability.
type funcCheck struct {
	id  string
	run func(ctx context.Context) Result
	fix func(ctx context.Context) error
}

func (f *funcCheck) ID() string                     { return f.id }
func (f *funcCheck) Run(ctx context.Context) Result { return f.run(ctx) }
func (f *funcCheck) Fix(ctx context.Context) error {
	if f.fix != nil {
		return f.fix(ctx)
	}
	return nil
}
