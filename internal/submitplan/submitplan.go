package submitplan

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	posixpath "path"
	"sort"
	"strconv"
	"strings"

	"github.com/gliese129/runq-lab/internal/job"
	"github.com/gliese129/runq-lab/internal/preflight"
	"github.com/gliese129/runq-lab/internal/project"
	"github.com/gliese129/runq-lab/internal/rfs"
	"github.com/gliese129/runq-lab/internal/utils"
	"github.com/gliese129/runq-lab/internal/workspace"
)

// Paths are backend-selected roots. Build only joins below these roots; it does
// not decide where daemon or HPC deployments should place their files.
type Paths struct {
	WorkspaceRoot string
	LogRoot       string
}

type Deps struct {
	// JobID is supplied by the backend, not generated here. The backend roots
	// its workspace on the job id (HPC uses ~/.runq/<job_id>/<task_id>), so it
	// must know the id before Build computes task dirs. When empty, Build falls
	// back to IDGen for backward compatibility.
	JobID string
	IDGen func() string // used for task ids (and job id only when JobID is empty)
	Paths Paths
	// PreflightDisableLocal skips env activation on the target — wired
	// from hpc `preflight_local: false` for strict login nodes.
	PreflightDisableLocal bool
	// PreflightScope labels where target checks ran (e.g. "on login node").
	PreflightScope string
	// PreflightFS is the filesystem preflight checks run against — the
	// TARGET's rfs.FS for remote targets (one exec on the login node).
	// nil = local os.
	PreflightFS rfs.FS
	// PreflightEnvSetup is the target's env_setup fragment: preflight
	// runs it before env activation, exactly like run.sh — ONE injection
	// point shared by preflight and run (RQ-76 core contract).
	PreflightEnvSetup string
	// SchedulerParams are param names consumed by the HPC submit_template
	// ({{param.*}}): exempt from the command renderer's unconsumed check
	// and excluded from {{args}} injection.
	SchedulerParams []string
	// DashboardBase is the dashboard's base URL when runq serves one ("" =
	// none): each task gets RUNQ_RESULTS_URL, its experiment's results page.
	DashboardBase string
}

type Plan struct {
	Target           string          `json:"target"`
	TargetSource     string          `json:"target_source"`
	CommandTemplate  string          `json:"command"`
	EffectiveConfig  *project.Config `json:"effective_config"`
	UnconsumedParams []string        `json:"unconsumed_params"`
	// SchedulerParams are the params only the scheduler reads (scope:
	// scheduler or used by submit_template): not part of a cell's identity.
	SchedulerParams []string `json:"scheduler_params,omitempty"`
	// Append is set by an append preview (--into): what each row would do.
	Append *job.AppendSummary `json:"append,omitempty"`

	JobID       string               `json:"job_id"`
	Project     string               `json:"project"`
	Description string               `json:"description"`
	Name        string               `json:"name"`
	GPUsPerTask int                  `json:"gpus_per_task"`
	Wandb       *project.WandbConfig `json:"wandb"`
	Tasks       []PlannedTask        `json:"tasks"`
	// Baseline is the reference run (rows describe themselves against it).
	Baseline  string   `json:"baseline"`
	SweepKeys []string `json:"sweep_keys"` // swept parameter names (sorted), for WANDB_TAGS/RUN_NAME
	// Preflight is the warning-only report (passed / warning / skipped);
	// it never aborts Build. Callers print it so skips stay visible; the
	// target backend adds shell_syntax once run.sh is rendered.
	Preflight preflight.Report `json:"preflight"`
	// Env is the merged JOB-level environment (project environment +
	// override env + the reserved RUNQ_ENV_FILE) — what setup_command and
	// the submit prelude export. Per-task rows add their RUNQ_* identity
	// on top (PlannedTask.Env).
	Env map[string]string `json:"env"`
}

type PlannedTask struct {
	TaskID        string            `json:"task_id"`
	RunName       string            `json:"run_name"`
	NeedsCode     []string          `json:"needs_code,omitempty"`
	Name          string            `json:"name"` // scheduler job name ({{name}} in submit_template), pre-sanitized
	Command       string            `json:"command"`
	Params        job.TaskParams    `json:"params"`
	Env           map[string]string `json:"env"`
	GPUsNeeded    int               `json:"gpus_needed"`
	Timeout       int               `json:"timeout"`
	LogPath       string            `json:"log_path"`
	WorkingDir    string            `json:"working_dir"`
	UID           int               `json:"uid"`
	TaskDir       string            `json:"task_dir"`
	CheckpointDir string            `json:"checkpoint_dir"`
	OutputDir     string            `json:"output_dir"`
	WandbDir      string            `json:"wandb_dir"`
}

// validateChoices enforces every nonempty choices list: a value outside the
// project's Choices list is a submit-time error. Comparison is on the
// string form (choices are stored as strings; params may be typed).
func validateChoices(proj *project.Config, tasks []job.TaskParams) error {
	for _, def := range proj.Params {
		if len(def.Choices) == 0 {
			continue
		}
		allowed := make(map[string]bool, len(def.Choices))
		for _, c := range def.Choices {
			allowed[c] = true
		}
		for _, params := range tasks {
			v, present := params[def.Name]
			if !present {
				continue
			}
			s := job.FormatValue(v)
			if !allowed[s] {
				return fmt.Errorf(
					"param %q: value %q is not among its choices (%s)",
					def.Name, s, strings.Join(def.Choices, ", "))
			}
		}
	}
	return nil
}

// validateParamTypes is the last line of defense for the param value
// chain (feedback group 2): an int/float param whose value cannot parse
// — the canonical case is the string "None" replayed from an old job
// config — is rejected AT SUBMIT with the param named, instead of
// argparse rejecting the task hours later on a compute node.
func validateParamTypes(proj *project.Config, tasks []job.TaskParams) error {
	for _, def := range proj.Params {
		if def.Type != "int" && def.Type != "float" && def.Type != "bool" {
			continue
		}
		for _, params := range tasks {
			v, present := params[def.Name]
			if !present {
				continue
			}
			s := strings.TrimSpace(job.FormatValue(v))
			var ok bool
			switch def.Type {
			case "int":
				_, err := strconv.ParseInt(s, 10, 64)
				ok = err == nil
			case "float":
				_, err := strconv.ParseFloat(s, 64)
				ok = err == nil
			case "bool":
				switch strings.ToLower(s) {
				case "true", "false", "1", "0", "yes", "no", "y", "n", "on", "off":
					ok = true
				}
			}
			if !ok {
				hint := ""
				if s == "None" || s == "" {
					hint = " — an unset optional param must be OMITTED, not sent as " + strconv.Quote(s)
				}
				return fmt.Errorf("param %q (%s) got non-%s value %q%s",
					def.Name, def.Type, def.Type, s, hint)
			}
		}
	}
	return nil
}

// resolveEnvFile applies the project's env_file semantics: nil = auto-use
// <working_dir>/.env when present; "" = disabled; other = required path.
// Existence questions go to fsys — the TARGET's filesystem (RQ-65): the
// .env lives next to the code, and the code lives on the target.
func resolveEnvFile(proj *project.Config, fsys rfs.FS) (string, error) {
	if proj.EnvFile == nil {
		auto := posixpath.Join(proj.WorkingDir, ".env")
		if _, err := fsys.Stat(auto); err == nil {
			return auto, nil
		}
		return "", nil
	}
	// Pre-rc6 registrations may still carry env_file (see
	// project.NormalizeLegacy): .env is the default anyway; anything else
	// would change what runs if ignored, so it stops the submit.
	cp := *proj
	if _, err := project.NormalizeLegacy(&cp); err != nil {
		return "", fmt.Errorf("project %s: %w; then run: runq project <project.yaml>", proj.ProjectName, err)
	}
	cp.EnvFile = nil
	return resolveEnvFile(&cp, fsys)
}

// Build compiles a JobConfig plus an already-resolved Project into a
// backend-neutral plan. It has no persistent side effects: no workspace
// creation, no DB writes, no scheduler/queue interaction.
func Build(ctx context.Context, cfg job.JobConfig, proj *project.Config, d Deps) (Plan, error) {
	if proj == nil {
		return Plan{}, fmt.Errorf("project config is nil")
	}
	var err error
	proj, err = job.EffectiveProject(cfg, proj)
	if err != nil {
		return Plan{}, err
	}
	if strings.TrimSpace(proj.CmdTemplate) == "" {
		return Plan{}, fmt.Errorf("command is required in job or project")
	}
	idGen := d.IDGen
	if idGen == nil {
		idGen = utils.GenerateID
	}

	for _, def := range proj.Params {
		for _, reserved := range job.ReservedRowKeys {
			if def.Name == reserved {
				return Plan{}, fmt.Errorf("project param %q clashes with the job.yaml row key %q; rename the param", def.Name, reserved)
			}
		}
	}
	// Scheduler params: DECLARED scope wins (self-describing config);
	// template-reference inference remains as a fallback for configs that
	// haven't adopted scope yet. Resolved before the rows: they are not
	// part of a cell, so generated cell names leave them out.
	schedParams := make(map[string]bool, len(d.SchedulerParams))
	for _, n := range d.SchedulerParams {
		schedParams[n] = true
	}
	for _, def := range proj.Params {
		if def.Scope == "scheduler" {
			schedParams[def.Name] = true
		}
	}
	rows, err := job.PlanRows(&cfg, schedParams)
	if err != nil {
		return Plan{}, fmt.Errorf("expand job: %w", err)
	}
	taskParams := make([]job.TaskParams, len(rows))
	for i, r := range rows {
		taskParams[i] = r.Params
	}

	gpusPerTask := proj.Defaults.GPUsPerTask
	if cfg.Overrides != nil {
		if cfg.Overrides.GPUsPerTask != nil {
			gpusPerTask = *cfg.Overrides.GPUsPerTask
		}

	}
	if gpusPerTask < 0 {
		gpusPerTask = 0
	}

	var timeoutSec int
	timeoutStr := proj.Defaults.Timeout
	if cfg.Overrides != nil && cfg.Overrides.Timeout != nil {
		timeoutStr = *cfg.Overrides.Timeout
	}
	if timeoutStr != "" {
		duration, err := utils.ParseHumanDuration(timeoutStr)
		if err != nil {
			return Plan{}, fmt.Errorf("invalid timeout %q: %w", timeoutStr, err)
		}
		timeoutSec = int(duration.Seconds())
	}

	env := make(map[string]string)
	for k, v := range proj.Environment {
		env[k] = v
	}
	if cfg.Overrides != nil {
		for k, v := range cfg.Overrides.Env {
			env[k] = v
		}
	}
	// Storage contract: runq owns where task data lives. Redirecting one of
	// its directories through env silently splits a task's data across disks
	// (and relative values land in the repo).
	for _, k := range workspace.RunqDirVars {
		if _, ok := env[k]; ok {
			return Plan{}, fmt.Errorf("%s is set in environment/overrides.env, but runq owns it (it points into the task directory). Remove it: set data_path on the target to move all task data, or pass your own path to the script as a flag", k)
		}
	}

	// working_dir existence is the TARGET filesystem's question (RQ-65) —
	// a Mac daemon statting a TSUBAME path answers about the wrong world.
	fsys := d.PreflightFS
	if fsys == nil {
		fsys = rfs.NewLocalFS()
	}
	if _, err := fsys.Stat(proj.WorkingDir); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Plan{}, fmt.Errorf("working_dir %q does not exist on target", proj.WorkingDir)
		}
		return Plan{}, fmt.Errorf("stat working_dir %q: %w", proj.WorkingDir, err)
	}

	// Ambient env file: carried as the reserved RUNQ_ENV_FILE env key so it
	// flows through EnvJSON persistence / daemon recovery untouched. The
	// shell sources it AT TASK START (executor prologue / run.sh header) —
	// runq never reads its values. Precedence: .env < explicit env.
	envFile, err := resolveEnvFile(proj, fsys)
	if err != nil {
		return Plan{}, err
	}
	if envFile != "" {
		env["RUNQ_ENV_FILE"] = envFile
	}

	if err := validateChoices(proj, taskParams); err != nil {
		return Plan{}, err
	}
	if err := validateParamTypes(proj, taskParams); err != nil {
		return Plan{}, err
	}

	pf := preflight.DefaultPreflight()
	pf.DisableLocal = d.PreflightDisableLocal
	pf.Scope = d.PreflightScope
	pf.ExcludeParams = schedParams
	pf.FS = d.PreflightFS
	// The probe checks THE SAME environment the task gets: merged env
	// (project + overrides + RUNQ_ENV_FILE) and the target's env_setup.
	pf.Env = env
	pf.EnvSetup = d.PreflightEnvSetup
	pf.DataDir = d.Paths.WorkspaceRoot
	pfReport, err := pf.Run(ctx, proj, taskParams)
	if err != nil {
		return Plan{}, err
	}

	jobID := d.JobID
	if jobID == "" {
		jobID = utils.GenerateJobID()
	}
	callerUID := os.Getuid()
	// Scheduler job name template: project.yaml job_name > default
	// rq-{{task_id}}. Rendered per task (params differ), always sanitized —
	// qsub/sbatch must never reject what runq generated. The job's display
	// name is free text and never feeds the scheduler.
	nameTmpl := proj.JobName
	rules := job.CommandRules{Exclude: schedParams, Flags: proj.FlagParams()}
	unconsumed := map[string]bool{}
	tasks := make([]PlannedTask, 0, len(taskParams))
	for i, params := range taskParams {
		for _, key := range job.UnconsumedParams(proj.CmdTemplate, params, schedParams) {
			unconsumed[key] = true
		}
		cmd, err := rules.Render(proj.CmdTemplate, params)
		if err != nil {
			return Plan{}, fmt.Errorf("render command failed: %w", err)
		}
		cmd = utils.WrapCommand(proj.PythonEnv.Type, proj.PythonEnv.Path, proj.PythonEnv.Name, cmd, proj.WorkingDir)

		taskID := idGen()
		taskDir := workspace.TaskDir(d.Paths.WorkspaceRoot, taskID)
		name := job.RenderJobName(nameTmpl, params, map[string]string{
			"project": cfg.Project, "job_id": jobID, "task_id": taskID,
		})
		tasks = append(tasks, PlannedTask{
			TaskID:        taskID,
			RunName:       rows[i].Run,
			NeedsCode:     rows[i].NeedsCode,
			Name:          name,
			Command:       cmd,
			Params:        params,
			Env:           cloneEnv(env),
			GPUsNeeded:    gpusPerTask,
			Timeout:       timeoutSec,
			LogPath:       posixpath.Join(d.Paths.LogRoot, taskID+".log"),
			WorkingDir:    proj.WorkingDir,
			UID:           callerUID,
			TaskDir:       taskDir,
			CheckpointDir: workspace.CheckpointsDir(taskDir),
		})
	}

	// Derive sweep keys: unique parameter names across all sweep blocks.
	var sweepKeys []string
	seenKey := make(map[string]bool)
	for _, block := range cfg.Sweep {
		for k := range block.Parameters {
			if !seenKey[k] {
				seenKey[k] = true
				sweepKeys = append(sweepKeys, k)
			}
		}
	}
	sort.Strings(sweepKeys)
	if len(cfg.Tasks) > 0 {
		sweepKeys = job.VaryingKeys(rows)
	}
	for i := range tasks {
		task := &tasks[i]
		task.Env = workspace.TaskEnv(workspace.Identity{TaskID: task.TaskID, JobID: jobID, Project: cfg.Project, TaskDir: task.TaskDir, SweepKeys: sweepKeys, JobName: cfg.Name, RunName: task.RunName, WandbConfigured: proj.Wandb != nil,
			ResultsURL: resultsURL(d.DashboardBase, cfg.Project, jobID)}, env, task.WorkingDir)
		task.CheckpointDir = task.Env["RUNQ_CHECKPOINT_DIR"]
		task.OutputDir = task.Env["RUNQ_OUTPUT_DIR"]
		task.WandbDir = task.Env["WANDB_DIR"]
	}

	keys := make([]string, 0, len(unconsumed))
	for key := range unconsumed {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	sched := make([]string, 0, len(schedParams))
	for name := range schedParams {
		sched = append(sched, name)
	}
	sort.Strings(sched)
	return Plan{
		Target: cfg.Target, TargetSource: cfg.TargetSource, CommandTemplate: proj.CmdTemplate,
		EffectiveConfig: proj, UnconsumedParams: keys, SchedulerParams: sched,
		JobID:       jobID,
		Project:     cfg.Project,
		Description: cfg.Description,
		Name:        cfg.Name,
		GPUsPerTask: gpusPerTask,
		Wandb:       proj.Wandb,
		Tasks:       tasks,
		Baseline:    baselineRun(rows, cfg.Baseline),
		SweepKeys:   sweepKeys,
		Preflight:   pfReport,
		Env:         cloneEnv(env),
	}, nil
}

func cloneEnv(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func baselineRun(rows []job.Row, baseline string) string {
	if len(rows) == 0 {
		return ""
	}
	return rows[job.BaselineIndex(rows, baseline)].Run
}

// resultsURL is an experiment's Results tab ("" without a dashboard).
func resultsURL(base, project, jobID string) string {
	page := utils.DashboardPage(base, "projects", project, jobID)
	if page == "" || project == "" {
		return ""
	}
	return page + "?tab=results"
}
