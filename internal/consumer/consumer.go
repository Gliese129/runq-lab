package consumer

import (
	_ "embed"
	"fmt"
	"regexp"
	"strings"
)

//go:embed runner.py
var Runner []byte

type Config struct {
	Name    string            `json:"name" yaml:"name"`
	Command string            `json:"command" yaml:"command"`
	Env     map[string]string `json:"env,omitempty" yaml:"env,omitempty"`
}
type Consumer struct {
	ID               string            `json:"id"`
	JobID            string            `json:"job_id"`
	Name             string            `json:"name"`
	Command          string            `json:"command"`
	Env              map[string]string `json:"env,omitempty"`
	Target           string            `json:"target"`
	Generation       string            `json:"target_generation"`
	Dir              string            `json:"dir"`
	RunSH            string            `json:"run_sh"`
	Status           string            `json:"status"`
	Host             string            `json:"host,omitempty"`
	PID              int               `json:"pid,omitempty"`
	PGID             int               `json:"pgid,omitempty"`
	PIDStart         string            `json:"pid_start,omitempty"`
	ProcessExited    bool              `json:"process_exited,omitempty"`
	StartedAt        *int64            `json:"started_at"`
	FinishedAt       *int64            `json:"finished_at"`
	LastHeartbeatAt  *int64            `json:"last_heartbeat_at"`
	LastOutputAt     *int64            `json:"last_output_at"`
	LastDataAt       *int64            `json:"last_data_at"`
	ExitCode         *int              `json:"exit_code"`
	Reason           string            `json:"reason,omitempty"`
	ObservedAt       *int64            `json:"observed_at"`
	ObservationState string            `json:"observation_state"`
	FinalIngest      bool              `json:"-"`
}

func Terminal(s string) bool { return s == "stopped" || s == "failed" || s == "killed" }

//go:embed python_entry.py
var PythonEntry []byte

var validName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`)
var validEnv = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func (c Config) Validate() error {
	if !validName.MatchString(c.Name) || strings.TrimSpace(c.Command) == "" {
		return fmt.Errorf("consumer requires a simple name and command")
	}
	for k := range c.Env {
		if !validEnv.MatchString(k) || strings.HasPrefix(k, "RUNQ_") {
			return fmt.Errorf("invalid or reserved consumer env key %q", k)
		}
	}
	return nil
}
