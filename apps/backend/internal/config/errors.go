package config

import "fmt"

// Error names the offending variable and a reason; it never carries the value.
type Error struct {
	Variable string
	Reason   string
}

func (e *Error) Error() string { return fmt.Sprintf("config: %s: %s", e.Variable, e.Reason) }
