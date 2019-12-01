package debouncer

import (
	"time"
)

type debounce struct {
	wait  time.Duration
	timer *time.Timer
}

/**
  Returns a debouncer
*/
func New(wait time.Duration) func(f func()) {
	d := &debounce{wait: wait}
	return func(f func()) {
		if d.timer != nil {
			d.timer.Stop()
		}
		d.timer = time.AfterFunc(d.wait, f)
	}
}
