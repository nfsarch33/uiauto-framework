package doctor

import (
	"fmt"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// CheckMetricsRegisterable verifies that fn registers collectors on a fresh registry without panicking.
func CheckMetricsRegisterable(name string, fn func(prometheus.Registerer)) Check {
	start := time.Now()
	var st Status = StatusPass
	var msg string
	func() {
		defer func() {
			if r := recover(); r != nil {
				st = StatusFail
				msg = fmt.Sprintf("panic: %v", r)
			}
		}()
		reg := prometheus.NewRegistry()
		fn(reg)
		if st == StatusPass {
			msg = "metrics registerable"
		}
	}()
	if msg == "" {
		msg = "metrics registerable"
	}
	return Check{Name: name, Status: st, Message: msg, Duration: time.Since(start)}
}
