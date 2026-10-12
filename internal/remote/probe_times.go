package remote

import (
	"strconv"
	"strings"
	"time"
)

// Parser extensions use explicit epoch seconds or RFC3339 with an offset.
// Bare local dates are left unknown: the controller need not share target TZ.
func parseProbeTimes(pr *ProbeResult, fields []string) {
	for _, field := range fields {
		k, v, ok := strings.Cut(field, "=")
		if !ok {
			continue
		}
		at, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			parsed, e := time.Parse(time.RFC3339, v)
			if e != nil {
				continue
			}
			at = parsed.Unix()
		}
		if at <= 0 {
			continue
		}
		switch k {
		case "submitted_at":
			pr.SubmittedAt = at
		case "started_at":
			if pr.Signal == SchedRunning || pr.Signal == SchedSuccess || pr.Signal == SchedFailed || pr.Signal == SchedKilled {
				pr.StartedAt = at
			}
		case "finished_at":
			if pr.Signal == SchedSuccess || pr.Signal == SchedFailed || pr.Signal == SchedKilled {
				pr.FinishedAt = at
			}
		}
	}
}
