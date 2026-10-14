package backend

import (
	"context"
	"encoding/json"
	"fmt"
	"path"
	"time"

	"github.com/gliese129/runq-lab/internal/utils"
	"github.com/gliese129/runq-lab/internal/views"
)

// Bound output and timeout where the command runs, before transport buffering.
const viewScriptRunner = `import json,os,selectors,signal,subprocess,sys,time
command,limit=sys.argv[1],float(sys.argv[2])
p=subprocess.Popen(command,shell=True,executable='/bin/bash',stdout=subprocess.PIPE,stderr=subprocess.PIPE,start_new_session=True)
sel=selectors.DefaultSelector();sel.register(p.stdout,selectors.EVENT_READ,'out');sel.register(p.stderr,selectors.EVENT_READ,'err')
out=bytearray();err=bytearray();failure='';deadline=time.monotonic()+limit
try:
 while sel.get_map():
  if time.monotonic()>deadline: failure='script timed out';break
  for key,_ in sel.select(min(.1,max(0,deadline-time.monotonic()))):
   chunk=os.read(key.fileobj.fileno(),65536)
   if not chunk:sel.unregister(key.fileobj);continue
   if key.data=='out':
    out.extend(chunk)
    if len(out)>2097152:failure='stdout exceeds 2 MB';break
   else:err.extend(chunk);err=err[-65536:]
  if failure:break
 if failure:
  os.killpg(p.pid,signal.SIGKILL);p.wait()
 else:
  p.wait(timeout=max(.01,deadline-time.monotonic()))
  if p.returncode:failure='script exit '+str(p.returncode)
 value=None
 if not failure:
  try:value=json.loads(out)
  except Exception:failure='stdout is not one JSON value'
 print(json.dumps({'value':value,'error':failure,'stderr':'\n'.join(err.decode(errors='replace').splitlines()[-20:])},allow_nan=False))
finally:
 if p.poll() is None:os.killpg(p.pid,signal.SIGKILL);p.wait()
`

func (m *MultiBackend) RunViewScript(ctx context.Context, project string, d views.DataSource) (any, string, error) {
	cfg, err := m.GetProject(ctx, project)
	if err != nil {
		return nil, "", err
	}
	be, err := m.resolve(cfg.Target)
	if err != nil {
		return nil, "", err
	}
	lane, ok := be.(*SSHBackend)
	if !ok {
		return nil, "", ErrNotSupported
	}
	timeout := 60 * time.Second
	if d.Timeout != "" {
		timeout, err = time.ParseDuration(d.Timeout)
		if err != nil {
			return nil, "", err
		}
	}
	ctx, cancel := context.WithTimeout(ctx, timeout+5*time.Second)
	defer cancel()
	env := cfg.Environment
	command := utils.WrapCommand(cfg.PythonEnv.Type, cfg.PythonEnv.Path, cfg.PythonEnv.Name, d.Run, cfg.WorkingDir)
	envFile := path.Join(cfg.WorkingDir, ".env")
	if cfg.EnvFile != nil {
		envFile = *cfg.EnvFile
		if envFile != "" && !path.IsAbs(envFile) {
			envFile = path.Join(cfg.WorkingDir, envFile)
		}
	}
	prelude := (utils.EnvPrelude{EnvSetup: lane.backend.Cfg.EnvSetup, EnvFile: envFile, Env: env}).Render()
	script := "set -e\ncd " + utils.ShellQuote(cfg.WorkingDir) + "\n" + prelude + "python3 -c " + utils.ShellQuote(viewScriptRunner) + " " + utils.ShellQuote(command) + " " + fmt.Sprint(timeout.Seconds())
	stdout, stderr, code, err := lane.backend.FS.Exec(ctx, "bash", "-c", script)
	if err != nil {
		return nil, string(stderr), err
	}
	if code != 0 {
		return nil, string(stderr), fmt.Errorf("script runner exited %d", code)
	}
	var result struct {
		Value  any    `json:"value"`
		Error  string `json:"error"`
		Stderr string `json:"stderr"`
	}
	if err = json.Unmarshal(stdout, &result); err != nil {
		return nil, "", err
	}
	if result.Error != "" {
		return result.Value, result.Stderr, fmt.Errorf("%s", result.Error)
	}
	return result.Value, result.Stderr, nil
}
