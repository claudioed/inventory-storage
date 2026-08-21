package memory

import "time"

// SystemClock implements ports.Clock using wall-clock time.
type SystemClock struct{}

func (SystemClock) Now() time.Time { return time.Now() }

// FixedClock implements ports.Clock with a fixed, settable time, for tests.
type FixedClock struct {
	t time.Time
}

func NewFixedClock(t time.Time) *FixedClock {
	return &FixedClock{t: t}
}

func (c *FixedClock) Now() time.Time { return c.t }

func (c *FixedClock) Advance(d time.Duration) {
	c.t = c.t.Add(d)
}
