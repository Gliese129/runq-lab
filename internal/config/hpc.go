package config

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/gliese129/runq-lab/internal/utils"
	"gopkg.in/yaml.v3"
)

// hpcConfigFile is the on-disk shape used for parsing. Only the HPC section
// is extracted here; global keys are handled by the main config.go.
type hpcConfigFile struct {
	HPC TargetConfig `yaml:"hpc"`
}

// HPCLogDir is <ConfigDir>/logs — runq's own logs in HPC mode (operation log).
func HPCLogDir() string { return filepath.Join(ConfigDir(), "logs") }

// HPCOpLogPath is the append-only HPC operation log.
func HPCOpLogPath() string { return filepath.Join(HPCLogDir(), "runq.log") }

// HPCPresets returns the scheduler names WriteHPCTemplate understands.
func HPCPresets() []string { return []string{"slurm", "pbs", "sge", "tsubame", "abci", "runq"} }

// HPCPreset parses a named template's hpc section into a TargetConfig.
func HPCPreset(name string) (*TargetConfig, error) {
	body, ok := hpcSnippets[name]
	if !ok {
		return nil, fmt.Errorf("unknown preset %q (available: %s)", name, strings.Join(HPCPresets(), ", "))
	}
	var f hpcConfigFile
	if err := yaml.Unmarshal([]byte(body), &f); err != nil {
		return nil, fmt.Errorf("parse preset %q: %w", name, err)
	}
	return &f.HPC, nil
}

// HPCPlaceholders is the single source of truth for which {{vars}} each
// template accepts.
var HPCPlaceholders = map[string][]string{
	"submit_template": {"run_sh", "gpus", "timeout", "job_id", "task_id", "task_dir", "name", "project", "log_path"},
	"kill_template":   {"ext_id"},
	"status_template": {"ext_id"},
	"status_parser":   {"ext_id"},
}

// hpcParamRefRe finds {{param.x}} references.
var hpcParamRefRe = regexp.MustCompile(`\{\{\s*(param\.[\w\-]+)\s*}}`)

// HPCTemplateParamRefs returns the param names referenced as {{param.X}} or
// bound by a [[--flag]] switch segment in a template — these are consumed by
// the scheduler layer.
func HPCTemplateParamRefs(tmpl string) []string {
	var names []string
	seen := map[string]bool{}
	add := func(n string) {
		if !seen[n] {
			seen[n] = true
			names = append(names, n)
		}
	}
	for _, m := range hpcParamRefRe.FindAllStringSubmatch(tmpl, -1) {
		add(strings.TrimPrefix(m[1], "param."))
	}
	for _, n := range utils.SegmentSwitchParams(tmpl) {
		add(n)
	}
	return names
}

// ── Template constants ───────────────────────────────────────────────────

const hpcConfigHeader = `# runq config — global settings + HPC cluster templates.
# Regenerate for a specific scheduler with: runq hpc init --scheduler slurm|pbs|sge

# ── Global (read by daemon + HPC) ──────────────────────────────────────────
# data_path: optional physical storage root for task workspaces. When set,
#   <working_dir>/.runq is a symlink to <data_path>/<project>/. When empty,
#   <working_dir>/.runq/ is the real storage location.
# data_path: "/scratch/user/runq"
`

const hpcSectionHeader = `
# ── HPC cluster templates ──────────────────────────────────────────────────
# runq does NOT parse scheduler dialects; it only interpolates these templates
# (shell-safe) and applies submit_id_regex. status_parser is an optional LIST
# of filter stages: runq feeds the raw status output to stage 1 on stdin and
# pipes each stage into the next; the final line must print one of:
#   pending | running | success | failed | killed | gone
#
# status_list_template + status_list_parser: optional BATCH probe. Runs ONCE
# to get all user jobs (e.g. qstat, squeue -u $USER). Parser output must be
# "ext_id signal" per line. Jobs absent from the output are treated as gone.
# When set, dashboard bulk-refresh uses this instead of per-job probes.
`

const genericHPCBody = `
hpc:
  # Queue one task. Vars: {{run_sh}} {{gpus}} {{job_id}} {{task_id}} {{task_dir}} {{name}}
  submit_template: "sbatch --gpus={{gpus}} --job-name={{name}} {{run_sh}}"
  submit_id_regex: "Submitted batch job ([0-9]+)"

  # Optional scheduler probe (Var: {{ext_id}}). Empty → status from status.json alone.
  status_template: ""
  # Optional pipeline (each stage a shell filter; {{ext_id}} available).
  status_parser: []

  # Optional BATCH probe: one call returns ALL user jobs. When set, the
  # dashboard's periodic refresh uses this instead of per-job probes.
  # Parser output: "ext_id signal" per line. Absent ext_ids → gone.
  status_list_template: ""
  status_list_parser: []

  # Map scheduler-native states to canonical signals (pending|running|
  # success|failed|killed|gone). Keys are case-insensitive. Unmatched
  # tokens fall back to the hardcoded defaults.
  # signal_map:
  #   CONFIGURING: pending
  #   COMPLETING: running

  kill_template: "scancel {{ext_id}}"
`

const slurmHPCBody = `
hpc:
  submit_template: "sbatch --gpus={{gpus}} --job-name={{name}} {{run_sh}}"
  submit_id_regex: "Submitted batch job ([0-9]+)"

  status_template: "SLURM_TIME_FORMAT=%s sacct -n -X -j {{ext_id}} --parsable2 -o State,Submit,Start,End"
  status_parser:
    - awk -F'|' 'NR==1{print $1, "submitted_at=" $2, "started_at=" $3, "finished_at=" $4}'

  # Batch probe: one squeue call for all active jobs (dashboard refresh).
  # %P = partition (shown as Queue in the dashboard).
  status_list_template: "SLURM_TIME_FORMAT=%s squeue -u $USER -h -o '%i %T %P submitted_at=%V started_at=%S'"
  status_list_parser: []

  # Map Slurm-native states to canonical signals. Tokens not listed here
  # fall back to the hardcoded ParseSignal (which knows the common ones).
  signal_map:
    CONFIGURING: pending
    COMPLETING: running
    RESIZING: pending
    REQUEUED: pending
    SUSPENDED: running
    SPECIAL_EXIT: failed
    PREEMPTED: failed

  kill_template: "scancel {{ext_id}}"

  # Optional: contribute this cluster's GPU view to the client's aggregated
  # panel (local + every remote target). The command must print a JSON array
  # of {"index","name","mem_total_mb","mem_used_mb","util_percent"} objects.
  # Cluster-specific — e.g. synthesize from sinfo gres/gresused columns, or
  # point at a site-provided reporting script.
  # gpu_template: "my-cluster-gpu-report --json"
`

const pbsHPCBody = `
hpc:
  submit_template: "qsub -l ngpus={{gpus}} -N {{name}} {{run_sh}}"
  submit_id_regex: "([0-9]+)"

  status_template: "qstat -f {{ext_id}} 2>/dev/null"
  status_parser:
    - awk -F'= ' '/job_state/{print $2; f=1} END{if(!f) print "gone"}'
    - sed -e s/R/running/ -e s/E/running/ -e s/Q/pending/ -e s/H/pending/

  # Batch probe: one qstat call for all active jobs.
  # PBS Pro qstat columns: Job_ID Username Queue Jobname ... S ...
  # Output: ext_id signal queue
  status_list_template: "qstat"
  status_list_parser:
    - awk 'NR>5{sub(/\..*/,"",$1); s="unknown"; if($10=="R"||$10=="E") s="running"; else if($10=="Q"||$10=="H") s="pending"; print $1, s, $3}'

  kill_template: "qdel {{ext_id}}"
`

const sgeHPCBody = `
hpc:
  submit_template: "qsub -l gpu={{gpus}} -N {{name}} {{run_sh}}"
  submit_id_regex: "Your job ([0-9]+)"

  status_template: "qstat"
  status_parser:
    - awk -v id={{ext_id}} '$1==id{print $5; f=1} END{if(!f) print "gone"}'
    - sed -e s/r/running/ -e s/qw/pending/ -e s/Eqw/failed/

  # Batch probe: same qstat, but outputs all jobs at once.
  # SGE qstat $8 = queue@host; strip @host for the queue name.
  # Output: ext_id signal queue
  status_list_template: "qstat"
  status_list_parser:
    - awk 'NR>2{s="unknown"; if($5=="r") s="running"; else if($5=="qw") s="pending"; else if($5=="Eqw") s="failed"; q=$8; sub(/@.*/,"",q); print $1, s, q}'

  kill_template: "qdel {{ext_id}}"
`

const tsubameHPCBody = `
hpc:
  # TSUBAME (UGE). Per-task scheduler knobs live in the sweep and are
  # referenced as {{param.*}} — zip node_kind / h_rt columns with your tasks.
  # Set fixed_params.group in job.yaml to the charged TSUBAME group.
  # [[...]] is optional: set fixed_params.ar_id to run inside an advance
  # reservation; leave it out (or empty) and -ar is not passed.
  submit_template: "qsub -g {{param.group}} [[-ar {{param.ar_id}}]] -l {{param.node_kind}}=1 -l h_rt={{param.h_rt}} -N {{name}} -o {{task_dir}} -e {{task_dir}} {{run_sh}}"
  submit_id_regex: "Your job ([0-9]+)"

  status_template: "qstat"
  status_parser:
    - awk -v id={{ext_id}} '$1==id{print $5; f=1} END{if(!f) print "gone"}'
    - sed -e s/r/running/ -e s/qw/pending/ -e s/Eqw/failed/

  # Batch probe: same qstat, but outputs all jobs at once.
  # UGE qstat $8 = queue@host; strip @host for the queue name.
  # Output: ext_id signal queue
  status_list_template: "qstat"
  status_list_parser:
    - awk 'NR>2{s="unknown"; if($5=="r") s="running"; else if($5=="qw") s="pending"; else if($5=="Eqw") s="failed"; q=$8; sub(/@.*/,"",q); print $1, s, q}'

  kill_template: "qdel {{ext_id}}"
`

const abciHPCBody = `
hpc:
  # ABCI (PBS Pro). Per-task knobs (queue, walltime) come from the sweep via
  # {{param.*}}. $ABCI_GROUP comes from the login shell (or hardcode it).
  submit_template: "qsub -P $ABCI_GROUP -q {{param.node_kind}} -l select=1 -l walltime={{param.walltime}} -N {{name}} -o {{task_dir}} -e {{task_dir}} -- {{run_sh}}"
  submit_id_regex: "([0-9]+)"

  status_template: "qstat -f {{ext_id}} 2>/dev/null"
  status_parser:
    - awk -F'= ' '/job_state/{print $2; f=1} END{if(!f) print "gone"}'
    - sed -e s/R/running/ -e s/E/running/ -e s/Q/pending/ -e s/H/pending/

  # Batch probe: one qstat call for all active jobs.
  # PBS Pro qstat columns: Job_ID Username Queue Jobname ... S ...
  # Output: ext_id signal queue
  status_list_template: "qstat"
  status_list_parser:
    - awk 'NR>5{sub(/\..*/,"",$1); s="unknown"; if($10=="R"||$10=="E") s="running"; else if($10=="Q"||$10=="H") s="pending"; print $1, s, $3}'

  kill_template: "qdel {{ext_id}}"
`

// runqHPCBody drives a remote runqd (the "runq preset"): the server
// exposes submit/list/cancel adapters, so a runqd is just another
// command-driven scheduler — with the nice property that its status
// vocabulary already IS the canonical signal vocabulary (no signal_map, no
// parsers). `runqd list` reads the daemon's local SQLite, so probing is cheap;
// the client still applies the same hibernation etiquette as everywhere.
const runqHPCBody = `
hpc:
  submit_template: "runqd submit --gpus {{gpus}} --timeout {{timeout}} --task-id {{task_id}} --task-dir {{task_dir}} --name {{name}} --project {{project}} --log {{log_path}} {{run_sh}}"
  submit_id_regex: "submitted (\\S+)"

  # runqd list reads its own SQLite — an empty list is a real answer,
  # not a parse suspicion; skip the conservative per-job fallback.
  trust_empty_list: true

  # Per-task probe and batch probe both use runqd's compatibility vocabulary —
  # PENDING/RUNNING/SUCCESS/FAILED/KILLED map 1:1 onto canonical signals.
  status_template: "runqd list | awk -v id={{ext_id}} '$1==id{print $2}'"
  status_list_template: "runqd list"

  kill_template: "runqd cancel {{ext_id}}"

  # This server's GPUs feed the client's aggregated (local + remote) panel.
  gpu_template: "runqd gpu --json"
`

// Full templates = global header + hpc header + hpc body (for new files).
var hpcTemplates = map[string]string{
	"":        hpcConfigHeader + hpcSectionHeader + genericHPCBody,
	"slurm":   hpcConfigHeader + hpcSectionHeader + slurmHPCBody,
	"pbs":     hpcConfigHeader + hpcSectionHeader + pbsHPCBody,
	"sge":     hpcConfigHeader + hpcSectionHeader + sgeHPCBody,
	"tsubame": hpcConfigHeader + hpcSectionHeader + tsubameHPCBody,
	"abci":    hpcConfigHeader + hpcSectionHeader + abciHPCBody,
	"runq":    hpcConfigHeader + hpcSectionHeader + runqHPCBody,
}

// hpcSnippets maps a scheduler name to its hpc-only section (for appending).
var hpcSnippets = map[string]string{
	"":        hpcSectionHeader + genericHPCBody,
	"slurm":   hpcSectionHeader + slurmHPCBody,
	"pbs":     hpcSectionHeader + pbsHPCBody,
	"sge":     hpcSectionHeader + sgeHPCBody,
	"tsubame": hpcSectionHeader + tsubameHPCBody,
	"abci":    hpcSectionHeader + abciHPCBody,
	"runq":    hpcSectionHeader + runqHPCBody,
}
