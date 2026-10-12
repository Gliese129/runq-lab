package cli

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"runtime"
	"strings"
	"time"

	"github.com/gliese129/runq-lab/internal/api"
	"github.com/gliese129/runq-lab/internal/config"
	"github.com/gliese129/runq-lab/internal/rfs"
	"github.com/gliese129/runq-lab/internal/utils"
	"github.com/gliese129/runq-lab/internal/version"

	"github.com/spf13/cobra"
	"golang.org/x/crypto/ssh"
)

// ── runq connect：连接一个已配置的 remote target（RQ-45）──
//
// "connect" 是动词本义：跟 target 的 login node 建立可用的连接——试连、
// 装载远端 CLI、打开 socket 转发。它不负责添加 target：配置归 `runq
// target add`，连接归这里，target 不存在直接报错指向前者。
//
// 幂等：重跑 `runq connect <name>` 就是"刷新远端"——升级本地 runq 后用它
// 把新二进制推到 login node。
//
// 安全上它是 host key 信任的"仪式现场"：daemon 的后台连接一律严格校验
// known_hosts，而首次信任必须发生在有人看着指纹的地方——就是这里
// （TOFU 指纹确认，同意后写 known_hosts）。

func init() {
	// connectCmd itself lives under `runq target` (target_cmd.go). The
	// top-level `runq connect` is a thin alias — the first command a new
	// user runs deserves a short spelling.
	rootCmd.AddCommand(connectAliasCmd)
	for _, cmd := range []*cobra.Command{connectCmd, connectAliasCmd} {
		cmd.Flags().String("remote-binary", "", "Matching remote-platform binary for unpublished/development versions")
		cmd.Flags().Duration("timeout", 2*time.Minute, "Total timeout per target, including forward readiness")
	}
}

var connectAliasCmd = &cobra.Command{
	Use:          "connect [name...]",
	Short:        "Alias of `runq target connect`",
	Hidden:       true, // one spelling in help; the alias keeps old scripts working
	Args:         cobra.ArbitraryArgs,
	RunE:         runConnect,
	GroupID:      groupDiag,
	SilenceUsage: true,
}

var connectCmd = &cobra.Command{
	Use:   "connect [name...]",
	Short: "Connect configured targets: verify SSH, install the remote CLI, enable the socket forward",
	Args:  cobra.ArbitraryArgs,
	RunE:  runConnect,
	// Business errors ("connection failed") are not usage errors — don't
	// bury them under the flags listing.
	SilenceUsage: true,
}

func runConnect(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	names := args
	if len(names) == 0 {
		if n := promptLine("Target name: "); n != "" {
			names = []string{n}
		}
	}
	if len(names) == 0 {
		return fmt.Errorf("a target name is required")
	}

	// Multiple targets connect sequentially (TOFU prompts must not
	// interleave); one failure doesn't strand the rest.
	var failed []string
	seen := map[string]bool{}
	for _, name := range names {
		if seen[name] {
			continue
		}
		seen[name] = true
		if len(names) > 1 {
			fmt.Printf("\n── %s ──\n", name)
		}
		if err := connectOne(cmd, cfg, name); err != nil {
			fmt.Printf("✗ %s: %v\n", name, err)
			failed = append(failed, name)
		}
	}
	if len(failed) > 0 {
		return fmt.Errorf("failed to connect: %s", strings.Join(failed, ", "))
	}
	return nil
}

func connectOne(cmd *cobra.Command, cfg *config.GlobalConfig, name string) error {
	var tc *config.TargetConfig
	for i := range cfg.Targets {
		if cfg.Targets[i].Name == name {
			tc = &cfg.Targets[i]
			break
		}
	}
	if tc == nil {
		return fmt.Errorf("target %q not found — add it first: `runq target add %s --template=<scheduler> --host=<login-node>`", name, name)
	}
	if tc.SSH == nil {
		return fmt.Errorf("target %q has no ssh section (local target) — nothing to connect to", name)
	}

	timeout, _ := cmd.Flags().GetDuration("timeout")
	if timeout <= 0 {
		return fmt.Errorf("timeout must be positive")
	}
	ctx, cancel := context.WithTimeout(cmd.Context(), timeout)
	defer cancel()
	sshCfg, err := rfs.ResolveTargetSSH(tc.SSH.Host, tc.SSH.Port, tc.SSH.User, tc.SSH.Key, tc.SSH.ProxyJump)
	if err != nil {
		return fmt.Errorf("SSH configuration: %w", err)
	}
	fmt.Printf("Connecting to %s ...\n", sshCfg.Host)
	// RQ-74: `StrictHostKeyChecking accept-new` in ~/.ssh/config skips the
	// TOFU prompt — the user already told ssh to trust new hosts silently,
	// and runq follows their config instead of adding ceremony. One line
	// says so (self-report: silence is fine, invisibility is not).
	var hostKeys ssh.HostKeyCallback
	if sshCfg.HostKeyPolicy == rfs.HostKeyAcceptNew {
		fmt.Println("host key: new hosts auto-trusted (StrictHostKeyChecking accept-new in ~/.ssh/config)")
		hostKeys, err = rfs.AcceptNewHostKeyCallback()
	} else {
		hostKeys, err = rfs.TOFUHostKeyCallback(func(h, fingerprint string) bool {
			fmt.Printf("\nThe authenticity of host %q can't be established.\n", h)
			fmt.Printf("Key fingerprint: %s\n", fingerprint)
			fmt.Println("Verify it against the cluster's published fingerprint if you can.")
			return confirmYN("Trust this host and add it to ~/.ssh/known_hosts?")
		})
	}
	if err != nil {
		return err
	}
	sshCfg.HostKeyCallback = hostKeys
	sshCfg.IdleTimeout = time.Minute
	sshCfg.OperationTimeout = 30 * time.Second
	fsys := rfs.NewSSHFS(sshCfg).WithContext(ctx)
	defer fsys.Close()
	// A running controller is required before provisioning its scoped env.
	controller, err := localController(ctx)
	if err != nil {
		return fmt.Errorf("controller readiness: %w; start/restart the local service with the current binary", err)
	}
	if _, _, _, err := fsys.Exec(ctx, "true"); err != nil {
		return fmt.Errorf("connection failed: %w", err)
	}
	fmt.Println("✓ SSH verified")

	// ── provision the remote CLI and enable the forward ──
	binary, _ := cmd.Flags().GetString("remote-binary")
	socket, err := setupRemoteCLI(ctx, fsys, name, controller, binary)
	if err != nil {
		return fmt.Errorf("remote CLI setup: %w", err)
	}
	if !tc.RemoteCLI {
		tc.RemoteCLI = true
		if err := config.SaveGlobal(cfg); err != nil {
			return err
		}
		fmt.Printf("✓ remote_cli enabled for %q\n", name)
	}
	if err := notifyDaemonForward(ctx, name); err != nil {
		return fmt.Errorf("start forward: %w", err)
	}
	fmt.Println("socket forward starting; verifying the remote round trip ...")
	if err := waitForwardReady(ctx, fsys, socket, controller, name); err != nil {
		return fmt.Errorf("forward readiness: %w; inspect `runq doctor` and the service log (`runq serve status`)", err)
	}
	fmt.Println("✓ ready (controller and target verified through the remote socket)")
	return nil
}

// notifyDaemonForward asks the RUNNING daemon to start the forward against
// the config we just saved — the happy path needs no restart at all. Every
// other outcome degrades to an honest instruction: daemon not running →
// the forward comes up with it; lane missing (target added after daemon
// start) → restart is genuinely required; old daemon without the endpoint
// → restart covers it too.
func localController(ctx context.Context) (string, error) {
	resp, err := api.NewClient(getSocketPath()).Do(ctx, http.MethodGet, "/api/v1/health", nil)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var body struct {
		ControllerID string `json:"controller_id"`
	}
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("health returned %d", resp.StatusCode)
	}
	if err = json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", err
	}
	if len(body.ControllerID) != 12 {
		return "", fmt.Errorf("runq service has no controller identity; run `runq serve restart` (upgrade if it persists)")
	}
	return body.ControllerID, nil
}
func notifyDaemonForward(ctx context.Context, name string) error {
	resp, err := api.NewClient(getSocketPath()).Do(ctx, http.MethodPost, "/api/v1/targets/"+url.PathEscape(name)+"/connect", nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("runq service returned %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	return nil
}
func waitForwardReady(ctx context.Context, fsys *rfs.SSHFS, socket, controller, target string) error {
	var last error
	for {
		attempt, cancel := context.WithTimeout(ctx, 5*time.Second)
		transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) { return fsys.DialUnix(ctx, socket) }, DisableKeepAlives: true}
		client := &http.Client{Transport: transport}
		req, _ := http.NewRequestWithContext(attempt, http.MethodGet, "http://remote/api/v1/health", nil)
		resp, err := client.Do(req)
		if err == nil {
			var body struct {
				ControllerID string `json:"controller_id"`
				Target       string `json:"forward_target"`
				Version      string `json:"version"`
			}
			err = json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&body)
			_ = resp.Body.Close()
			if err == nil && resp.StatusCode == 200 && body.ControllerID == controller && body.Target == target && body.Version == version.Version {
				cancel()
				transport.CloseIdleConnections()
				return nil
			}
			if err == nil {
				err = fmt.Errorf("remote identity/version mismatch (expected controller %s target %s version %s)", controller, target, version.Version)
			}
		}
		last = err
		// Keep the supervisor's phase/error, not just a masked channel-open
		// failure, so a readiness deadline points to the actual remedy.
		if response, e := api.NewClient(getSocketPath()).Do(attempt, http.MethodGet, "/api/v1/health", nil); e == nil {
			var body struct {
				Forwards map[string]rfs.ForwardStatus `json:"forwards"`
			}
			_ = json.NewDecoder(io.LimitReader(response.Body, 65536)).Decode(&body)
			_ = response.Body.Close()
			if status, ok := body.Forwards[target]; ok && status.LastError != "" {
				last = fmt.Errorf("%s: %s", status.State, status.LastError)
			}
		}
		cancel()
		transport.CloseIdleConnections()
		select {
		case <-ctx.Done():
			return fmt.Errorf("%w: last round trip: %v", ctx.Err(), last)
		case <-time.After(500 * time.Millisecond):
		}
	}
}
func setupRemoteCLI(ctx context.Context, fsys *rfs.SSHFS, targetName, controller, binary string) (string, error) {
	home, err := fsys.Home()
	if err != nil {
		return "", err
	}
	dir := path.Join(home, utils.RemoteSessionDir(controller, targetName))
	socket := path.Join(home, utils.RemoteSocketPath(controller, targetName))
	if len([]byte(socket)) > 103 {
		return "", fmt.Errorf("remote socket path is too long (%d bytes; maximum 103)", len([]byte(socket)))
	}
	binPath := path.Join(home, ".runq/bin/runq")
	if err := fsys.MkdirAll(path.Dir(binPath), 0700); err != nil {
		return "", err
	}
	if err := fsys.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	stdout, _, exit, err := fsys.Exec(ctx, utils.ShellQuote(binPath)+" version")
	if binary != "" || err != nil || exit != 0 || strings.TrimSpace(string(stdout)) != version.Version || version.Version == "dev" {
		if err := installRemoteBinary(ctx, fsys, binPath, binary); err != nil {
			return "", err
		}
	}
	env := fmt.Sprintf("# Managed by runq connect; controller %s, target %s\nexport PATH=%s:\"$PATH\"\nexport RUNQ_SOCKET=%s\nexport RUNQ_TARGET=%s\n", controller, strings.ReplaceAll(targetName, "\n", " "), utils.ShellQuote(path.Dir(binPath)), utils.ShellQuote(socket), utils.ShellQuote(targetName))
	if err := fsys.WriteFile(path.Join(dir, "env"), []byte(env), 0600); err != nil {
		return "", err
	}
	fmt.Printf("✓ remote env: source %s\n", utils.ShellQuote(path.Join(dir, "env")))
	return socket, nil
}
func installRemoteBinary(ctx context.Context, fsys *rfs.SSHFS, binPath, binary string) error {
	stdout, _, exit, err := fsys.Exec(ctx, "uname", "-sm")
	if err != nil {
		return err
	}
	if exit != 0 {
		return fmt.Errorf("remote platform probe exited %d", exit)
	}
	fields := strings.Fields(string(stdout))
	if len(fields) != 2 {
		return fmt.Errorf("unsupported platform %q", stdout)
	}
	goos := strings.ToLower(fields[0])
	arch := strings.ToLower(fields[1])
	switch arch {
	case "x86_64":
		arch = "amd64"
	case "aarch64":
		arch = "arm64"
	}
	var data []byte
	if binary != "" {
		data, err = os.ReadFile(binary)
	} else if unameMatchesSelf(string(stdout)) {
		var self string
		self, err = os.Executable()
		if err == nil {
			data, err = os.ReadFile(self)
		}
	} else {
		if version.Version == "dev" || strings.Contains(version.Version, "+") {
			return fmt.Errorf("unpublished version %s: supply --remote-binary with a matching %s/%s build", version.Version, goos, arch)
		}
		asset := "runq-" + goos + "-" + arch
		base := "https://github.com/gliese129/runq-lab/releases/download/" + url.PathEscape(version.Version) + "/"
		var checksum []byte
		data, err = downloadRelease(ctx, base+asset, 128<<20)
		if err == nil {
			checksum, err = downloadRelease(ctx, base+asset+".sha256", 4096)
		}
		if err == nil {
			sum := sha256.Sum256(data)
			parts := strings.Fields(string(checksum))
			if len(parts) < 2 || parts[1] != asset || !strings.EqualFold(parts[0], hex.EncodeToString(sum[:])) {
				err = fmt.Errorf("release checksum mismatch for %s", asset)
			}
		}
	}
	if err != nil {
		return err
	}
	if len(data) == 0 {
		return fmt.Errorf("empty remote binary")
	}
	return installRemoteBinaryData(ctx, fsys, binPath, data)
}

// Installation owns only its uniquely named temporary file. Cleanup must be
// registered before the first write and must survive upload cancellation.
type remoteBinaryInstaller interface {
	WriteFileContext(context.Context, string, []byte, os.FileMode) error
	Exec(context.Context, string, ...string) ([]byte, []byte, int, error)
	Rename(string, string) error
	RemoveContext(context.Context, string) error
}

func installRemoteBinaryData(ctx context.Context, fsys remoteBinaryInstaller, binPath string, data []byte) error {
	var nonce [8]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return err
	}
	tmp := binPath + ".tmp-" + hex.EncodeToString(nonce[:])
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := fsys.RemoveContext(cleanup, tmp); err != nil && !errors.Is(err, fs.ErrNotExist) {
			slog.Warn("remote temporary binary cleanup failed", "path", tmp, "error", err)
		}
	}()
	fmt.Printf("Uploading matching remote CLI (%.1f MiB) ...\n", float64(len(data))/(1<<20))
	if err := fsys.WriteFileContext(ctx, tmp, data, 0755); err != nil {
		return fmt.Errorf("upload temporary executable: %w", err)
	}
	// Executing only the temporary file validates platform and exact version;
	// the current executable stays intact on every failure.
	out, se, exit, err := fsys.Exec(ctx, utils.ShellQuote(tmp)+" version")
	if err != nil {
		return fmt.Errorf("verify temporary executable: %w", err)
	}
	if exit != 0 || strings.TrimSpace(string(out)) != version.Version {
		return fmt.Errorf("temporary remote binary must report %s (exit %d): %s", version.Version, exit, strings.TrimSpace(string(se)))
	}
	if err := fsys.Rename(tmp, binPath); err != nil {
		return fmt.Errorf("atomic remote install: %w", err)
	}
	fmt.Printf("✓ remote runq %s installed\n", version.Version)
	return nil
}
func downloadRelease(ctx context.Context, url string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("release download returned %d; supply --remote-binary for unpublished versions", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("release asset exceeds size limit")
	}
	return data, err
}

// unameMatchesSelf reports whether `uname -sm` output describes the same
// platform as this build — the precondition for shipping our own binary.
func unameMatchesSelf(unameSM string) bool {
	fields := strings.Fields(unameSM)
	if len(fields) != 2 {
		return false
	}
	if !strings.EqualFold(fields[0], runtime.GOOS) {
		return false
	}
	arch := strings.ToLower(fields[1])
	switch runtime.GOARCH {
	case "amd64":
		return arch == "x86_64" || arch == "amd64"
	case "arm64":
		return arch == "aarch64" || arch == "arm64"
	default:
		return arch == runtime.GOARCH
	}
}

// promptLine reads one trimmed line from stdin.
func promptLine(prompt string) string {
	fmt.Print(prompt)
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	return strings.TrimSpace(line)
}
