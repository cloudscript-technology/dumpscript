package dumper

import "strings"

// ArgBuilder composes command-line arguments fluently.
// Empty strings are silently dropped so callers can Add optional flags unconditionally.
type ArgBuilder struct {
	args []string
}

func NewArgBuilder() *ArgBuilder { return &ArgBuilder{args: make([]string, 0, 8)} }

// Add appends one or more args, skipping empty strings.
func (b *ArgBuilder) Add(args ...string) *ArgBuilder {
	for _, a := range args {
		if a != "" {
			b.args = append(b.args, a)
		}
	}
	return b
}

// AddRaw splits a whitespace-separated raw string (e.g. DUMP_OPTIONS passthrough).
func (b *ArgBuilder) AddRaw(raw string) *ArgBuilder {
	for _, a := range strings.Fields(raw) {
		b.args = append(b.args, a)
	}
	return b
}

// Build returns the accumulated arguments.
func (b *ArgBuilder) Build() []string { return b.args }
