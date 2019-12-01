package debouncer

import (
	"testing"
	"time"
)

var (
	counter int
)

func TestDebouncer(t *testing.T) {

	debTester := func() {
		counter++
	}

	debounced := New(50 * time.Millisecond)

	counter = 0

	debounced(debTester)
	debounced(debTester)
	debounced(debTester)
	debounced(debTester)

	time.Sleep(55 * time.Millisecond)
	if counter != 1 {
		t.Error("Debouncer did't work", counter)
	}
}
