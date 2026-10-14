package backend

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"regexp"
	"time"

	"github.com/gliese129/runq-lab/internal/consumer"
	"github.com/gliese129/runq-lab/internal/ingest"
	"github.com/gliese129/runq-lab/internal/job"
	"github.com/gliese129/runq-lab/internal/logfile"
	"github.com/gliese129/runq-lab/internal/rfs"
	"github.com/gliese129/runq-lab/internal/store"
	"github.com/gliese129/runq-lab/internal/utils"
)

type Consumers interface {
	AddConsumer(context.Context, string, consumer.Config) (*consumer.Consumer, error)
	ListConsumers(context.Context, string) ([]consumer.Consumer, error)
	GetConsumer(context.Context, string) (*consumer.Consumer, error)
	KillConsumer(context.Context, string) error
	ConsumerLog(context.Context, string, logfile.PageRequest) (*LogPage, error)
}

func (m *MultiBackend) consumerLane(ctx context.Context, id string) (*SSHBackend, *consumer.Consumer, error) {
	c, err := m.store.GetConsumer(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	if c == nil {
		return nil, nil, ErrNotFound
	}
	be, err := m.resolveTaskRow(&store.TaskRow{Target: c.Target, TargetGeneration: c.Generation})
	if err != nil {
		return nil, c, err
	}
	lane, ok := be.(*SSHBackend)
	if !ok {
		return nil, c, ErrNotSupported
	}
	return lane, c, nil
}
func (m *MultiBackend) AddConsumer(ctx context.Context, id string, cfg consumer.Config) (*consumer.Consumer, error) {
	j, err := m.store.GetJob(ctx, id)
	if err != nil {
		return nil, err
	}
	if j == nil {
		return nil, ErrNotFound
	}
	be, err := m.resolve(j.Target)
	if err != nil {
		return nil, err
	}
	lane, ok := be.(*SSHBackend)
	if !ok {
		return nil, ErrNotSupported
	}
	return lane.AddConsumer(ctx, j.ID, cfg)
}
func (m *MultiBackend) ListConsumers(ctx context.Context, id string) ([]consumer.Consumer, error) {
	j, err := m.store.GetJob(ctx, id)
	if err != nil {
		return nil, err
	}
	if j == nil {
		return nil, ErrNotFound
	}
	return m.store.ListConsumers(ctx, j.ID, "")
}
func (m *MultiBackend) GetConsumer(ctx context.Context, id string) (*consumer.Consumer, error) {
	c, err := m.store.GetConsumer(ctx, id)
	if err == nil && c == nil {
		err = ErrNotFound
	}
	return c, err
}
func (m *MultiBackend) KillConsumer(ctx context.Context, id string) error {
	lane, c, err := m.consumerLane(ctx, id)
	if err != nil {
		return err
	}
	return lane.KillConsumer(ctx, c.ID)
}
func (m *MultiBackend) ConsumerLog(ctx context.Context, id string, req logfile.PageRequest) (*LogPage, error) {
	lane, c, err := m.consumerLane(ctx, id)
	if err != nil {
		return nil, err
	}
	return lane.ConsumerLog(ctx, c.ID, req)
}
func (m *MultiBackend) ConsumerTarget(ctx context.Context, id string) (string, error) {
	c, err := m.GetConsumer(ctx, id)
	if err != nil {
		return "", err
	}
	return c.Target, nil
}
func (b *SSHBackend) ListConsumers(ctx context.Context, id string) ([]consumer.Consumer, error) {
	return b.store.ListConsumers(ctx, id, b.targetName)
}
func (b *SSHBackend) GetConsumer(ctx context.Context, id string) (*consumer.Consumer, error) {
	c, err := b.store.GetConsumer(ctx, id)
	if err == nil && (c == nil || c.Target != b.targetName || c.Generation != b.Generation()) {
		return nil, ErrNotFound
	}
	return c, err
}

var pythonCommand = regexp.MustCompile(`^((?:'[^']*/python[0-9.]*'|'python[0-9.]*'|[^\s]*/python[0-9.]*|python[0-9.]*))(\s+)`)

func (b *SSHBackend) AddConsumer(ctx context.Context, id string, cfg consumer.Config) (*consumer.Consumer, error) {
	b.admissionsInFlight.Add(1)
	defer b.admissionsInFlight.Add(-1)
	if b.scope.IsRetiring() {
		return nil, ErrLaneRetired
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	j, err := b.store.GetJob(ctx, id)
	if err != nil {
		return nil, err
	}
	if j == nil || j.Target != b.targetName {
		return nil, ErrNotFound
	}
	existing, err := b.store.ListConsumers(ctx, j.ID, "")
	if err != nil {
		return nil, err
	}
	for _, c := range existing {
		if c.Name == cfg.Name {
			return nil, fmt.Errorf("consumer %q exists: runq consumer show %s; restart using its existing run.sh", cfg.Name, c.ID)
		}
	}
	tasks, err := b.store.ListTasks(ctx, store.TaskFilter{JobID: j.ID})
	if err != nil {
		return nil, err
	}
	if len(tasks) == 0 || tasks[0].TaskDir == "" {
		return nil, fmt.Errorf("job has no workspace")
	}
	proj, err := b.GetProject(ctx, j.ProjectName)
	if err != nil {
		return nil, err
	}
	var jobCfg job.JobConfig
	if json.Unmarshal([]byte(j.ConfigJSON), &jobCfg) == nil && jobCfg.EffectiveConfig != nil {
		proj = jobCfg.EffectiveConfig
	}
	nonce := make([]byte, 8)
	if _, err = rand.Read(nonce); err != nil {
		return nil, err
	}
	csID := "cs" + hex.EncodeToString(nonce)
	jobDir := path.Dir(tasks[0].TaskDir)
	dir := path.Join(jobDir, "consumers", cfg.Name+"-"+csID)
	c := &consumer.Consumer{ID: csID, JobID: j.ID, Name: cfg.Name, Command: cfg.Command, Env: cfg.Env, Target: b.targetName, Generation: b.Generation(), Dir: dir, RunSH: path.Join(dir, "run.sh"), Status: "ready", ObservationState: "never"}
	env := map[string]string{}
	for k, v := range proj.Environment {
		env[k] = v
	}
	for k, v := range cfg.Env {
		env[k] = v
	}
	for k, v := range map[string]string{"RUNQ_CONSUMER_ID": csID, "RUNQ_CONSUMER": cfg.Name, "RUNQ_CONSUMER_DIR": dir, "RUNQ_JOB_ID": j.ID, "RUNQ_JOB_DIR": jobDir, "RUNQ_OUTPUT_DIR": path.Join(dir, "outputs"), "RUNQ_METRICS_FILE": path.Join(dir, "metrics.jsonl"), "RUNQ_PROJECT": j.ProjectName, "RUNQ_NO_DAEMON": "1"} {
		env[k] = v
	}
	command := pythonCommand.ReplaceAllString(cfg.Command, "${1} "+utils.ShellQuote(path.Join(dir, "python_entry.py"))+" ")
	command = utils.WrapCommand(proj.PythonEnv.Type, proj.PythonEnv.Path, proj.PythonEnv.Name, "exec "+command, proj.WorkingDir)
	// Environment setup runs before launching, then exec leaves only the business
	// process and watcher. Wrappers from python_env must themselves end in exec.
	envFile := path.Join(proj.WorkingDir, ".env")
	if proj.EnvFile != nil {
		envFile = *proj.EnvFile
		if envFile != "" && !path.IsAbs(envFile) {
			envFile = path.Join(proj.WorkingDir, envFile)
		}
	}
	script := "#!/bin/bash\nset -e\nexec >> " + utils.ShellQuote(path.Join(dir, "log.txt")) + " 2>&1\ncd " + utils.ShellQuote(proj.WorkingDir) + "\n" + (utils.EnvPrelude{EnvSetup: b.backend.Cfg.EnvSetup, EnvFile: envFile, Env: env}).Render() + "exec python3 " + utils.ShellQuote(path.Join(dir, "runner.py")) + " \"$@\"\n"
	fsys := rfs.WithContext(b.backend.FS, ctx)
	if err = fsys.MkdirAll(path.Join(dir, "outputs"), 0700); err != nil {
		return nil, err
	}
	for name, data := range map[string][]byte{"runner.py": consumer.Runner, "python_entry.py": consumer.PythonEntry, "command.sh": []byte(command), "run.sh": []byte(script)} {
		if err = fsys.WriteFile(path.Join(dir, name), data, 0700); err != nil {
			return nil, err
		}
	}
	if err = b.writeConsumerSnapshot(ctx, fsys, *c); err != nil {
		return nil, err
	}
	if err = b.store.InsertConsumer(ctx, c); err != nil {
		return nil, fmt.Errorf("consumer name already reserved or persistence failed: %w", err)
	}
	return c, nil
}
func atomicRemote(ctx context.Context, fsys rfs.FS, name string, data []byte) error {
	nonce := make([]byte, 8)
	if _, err := rand.Read(nonce); err != nil {
		return err
	}
	tmp := name + ".tmp." + hex.EncodeToString(nonce)
	if err := fsys.WriteFile(tmp, data, 0600); err != nil {
		return err
	}
	_, stderr, code, err := fsys.Exec(ctx, "mv", "-f", tmp, name)
	if err != nil {
		return err
	}
	if code != 0 {
		return fmt.Errorf("publish %s: %s", name, stderr)
	}
	return nil
}
func (b *SSHBackend) writeConsumerSnapshot(ctx context.Context, fsys rfs.FS, c consumer.Consumer) error {
	tasks, err := b.store.ListTasks(ctx, store.TaskFilter{JobID: c.JobID})
	if err != nil {
		return err
	}
	rows := []map[string]any{}
	for _, t := range tasks {
		var params map[string]any
		_ = json.Unmarshal([]byte(t.ParamsJSON), &params)
		obs, err := b.store.SourceObservation(ctx, t.ID)
		if err != nil {
			return err
		}
		rows = append(rows, map[string]any{"task_id": t.ID, "params": params, "task_dir": t.TaskDir, "output_dir": path.Join(t.TaskDir, "outputs"), "status": t.Status, "started_at": unixPtr(t.StartedAt), "finished_at": unixPtr(t.FinishedAt), "step": obs.Step, "last_output_at": obs.LastOutputAt})
	}
	observed, observation, err := b.store.JobObservation(ctx, tasks)
	if err != nil {
		return err
	}
	raw, _ := json.Marshal(map[string]any{"tasks": rows, "updated_at": time.Now().Unix(), "observed_at": observed, "observation_state": observation})
	return atomicRemote(ctx, fsys, path.Join(c.Dir, "job.json"), raw)
}
func unixPtr(t *time.Time) *int64 {
	if t == nil {
		return nil
	}
	v := t.Unix()
	return &v
}
func (b *SSHBackend) reconcileConsumers(ctx context.Context) {
	list, err := b.store.ListConsumers(ctx, "", b.targetName)
	if err != nil {
		return
	}
	fsys := rfs.WithContext(b.backend.FS, ctx)
	for _, c := range list {
		if c.Generation != b.Generation() {
			continue
		}
		err = b.writeConsumerSnapshot(ctx, fsys, c)
		if err == nil {
			var raw []byte
			raw, err = fsys.ReadFile(path.Join(c.Dir, "status.json"))
			if os.IsNotExist(err) && c.Status == "ready" {
				err = nil
			} else if err == nil {
				var state consumer.Consumer
				err = json.Unmarshal(raw, &state)
				if err == nil {
					c.Status = state.Status
					c.Host = state.Host
					c.PID = state.PID
					c.PGID = state.PGID
					c.PIDStart = state.PIDStart
					c.ProcessExited = state.ProcessExited
					c.StartedAt = state.StartedAt
					c.FinishedAt = state.FinishedAt
					c.LastHeartbeatAt = state.LastHeartbeatAt
					c.LastOutputAt = state.LastOutputAt
					c.LastDataAt = state.LastDataAt
					c.ExitCode = state.ExitCode
					c.Reason = state.Reason
					if c.Status == "running" && c.LastHeartbeatAt != nil && time.Now().Unix()-*c.LastHeartbeatAt > 180 {
						c.Status = "lost"
						c.Reason = "process heartbeat expired; inspect host/pid before restarting this consumer"
					}
				}
			}
		}
		if err == nil {
			final := consumer.Terminal(c.Status) || c.ProcessExited
			err = ingest.ReapConsumer(ctx, b.store, c, fsys, final)
			if err == nil {
				c.FinalIngest = final
				now := time.Now().Unix()
				c.ObservedAt = &now
				c.ObservationState = "ok"
			}
		}
		if err != nil {
			c.ObservationState = "unconfirmed"
			c.Reason = "observation failed: " + err.Error()
		}
		if err := b.store.UpdateConsumer(ctx, &c); err != nil {
			b.logger.Warn("persist consumer observation", "consumer", c.ID, "error", err)
		}
	}
}
func (b *SSHBackend) KillConsumer(ctx context.Context, id string) error {
	c, err := b.GetConsumer(ctx, id)
	if err != nil {
		return err
	}
	_, stderr, code, err := b.backend.FS.Exec(ctx, "python3", path.Join(c.Dir, "runner.py"), "cancel")
	if err != nil {
		return err
	}
	if code != 0 {
		return fmt.Errorf("request cancellation: %s", stderr)
	}
	return nil
}
func (b *SSHBackend) ConsumerLog(ctx context.Context, id string, req logfile.PageRequest) (*LogPage, error) {
	c, err := b.GetConsumer(ctx, id)
	if err != nil {
		return nil, err
	}
	reader, err := logfile.Open(path.Join(c.Dir, "log.txt"), rfs.WithContext(b.backend.FS, ctx))
	if os.IsNotExist(err) {
		return &LogPage{Lines: []string{}}, nil
	}
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	return reader.ReadPage(req)
}

func consumerCounts(cs []consumer.Consumer) map[string]int {
	out := map[string]int{"total": len(cs)}
	for _, c := range cs {
		out[c.Status]++
	}
	return out
}
