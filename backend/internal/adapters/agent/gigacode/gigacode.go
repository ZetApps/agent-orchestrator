// Package gigacode implements the Sber GigaCode CLI agent adapter: launching
// new sessions, resuming hook-tracked sessions, and installing workspace-local
// native hooks.
//
// GigaCode CLI is a closed-source terminal agent. This adapter relies only on
// flags that `gigacode --help` documents: `-i/--prompt-interactive` runs the
// task and stays in the TUI, `-r/--resume <id>` continues a session,
// `--approval-mode {plan,default,auto-edit,auto}` selects permissions, and
// `-m/--model` selects a model. It also has a Claude-Code-shaped hook system in
// `.gigacode/settings.json`, so AO tracks activity and the native session id
// from hooks. GigaCode has no system-prompt flag, so the session's standing
// instructions are not applied at launch (same as the Gemini adapter).
package gigacode

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/aoagents/agent-orchestrator/backend/internal/adapters"
	"github.com/aoagents/agent-orchestrator/backend/internal/adapters/agent/agentbase"
	"github.com/aoagents/agent-orchestrator/backend/internal/adapters/agent/binaryutil"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

// Plugin is the GigaCode CLI adapter. It is safe for concurrent use; the binary
// path is resolved once and cached under binaryMu.
type Plugin struct {
	agentbase.Base
	binaryMu       sync.Mutex
	resolvedBinary string
}

// New returns a ready-to-register GigaCode CLI adapter.
func New() *Plugin { return &Plugin{} }

// InterruptKey uses Ctrl+C to cancel work without closing GigaCode.
func (*Plugin) InterruptKey() string { return "\x03" }

var _ adapters.Adapter = (*Plugin)(nil)
var _ ports.Agent = (*Plugin)(nil)
var _ ports.AgentBinaryResolutionInvalidator = (*Plugin)(nil)

// Manifest returns the adapter's static self-description.
func (p *Plugin) Manifest() adapters.Manifest {
	return adapters.Manifest{
		ID:           "gigacode",
		Name:         "GigaCode CLI",
		Description:  "Run Sber GigaCode CLI worker sessions.",
		Version:      "0.0.1",
		Capabilities: []adapters.Capability{adapters.CapabilityAgent},
	}
}

// GetConfigSpec exposes the native model override.
func (p *Plugin) GetConfigSpec(ctx context.Context) (ports.ConfigSpec, error) {
	return agentbase.ModelConfigSpec(ctx, "Model override passed to `gigacode --model`.")
}

// GetLaunchCommand starts an interactive GigaCode session. A non-empty prompt
// is delivered with `--prompt-interactive`, which runs it and keeps the TUI
// open, so the argument may safely begin with "-".
func (p *Plugin) GetLaunchCommand(ctx context.Context, cfg ports.LaunchConfig) ([]string, error) {
	cmd, err := p.command(ctx, cfg.Permissions, cfg.Config, cfg.AllowedTools, cfg.DisallowedTools)
	if err != nil {
		return nil, err
	}
	if cfg.Prompt != "" {
		cmd = append(cmd, "--prompt-interactive", cfg.Prompt)
	}
	return cmd, nil
}

// GetPromptDeliveryStrategy reports that the task travels in the launch argv.
func (p *Plugin) GetPromptDeliveryStrategy(ctx context.Context, _ ports.LaunchConfig) (ports.PromptDeliveryStrategy, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return ports.PromptDeliveryInCommand, nil
}

// GetRestoreCommand resumes the native session id recorded by GigaCode hooks.
// ok is false until the SessionStart hook has delivered that id, so callers fall
// back to fresh-launch behavior.
func (p *Plugin) GetRestoreCommand(ctx context.Context, cfg ports.RestoreConfig) ([]string, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	id := strings.TrimSpace(cfg.Session.Metadata[ports.MetadataKeyAgentSessionID])
	if id == "" {
		return nil, false, nil
	}
	cmd, err := p.command(ctx, cfg.Permissions, cfg.Config, cfg.AllowedTools, cfg.DisallowedTools)
	if err != nil {
		return nil, false, err
	}
	cmd = append(cmd, "--resume", id)
	if cfg.Prompt != "" {
		cmd = append(cmd, "--prompt-interactive", cfg.Prompt)
	}
	return cmd, true, nil
}

func (p *Plugin) command(ctx context.Context, mode ports.PermissionMode, cfg ports.AgentConfig, allow, deny []string) ([]string, error) {
	if len(allow) != 0 || len(deny) != 0 {
		return nil, fmt.Errorf("gigacode: tool restrictions are not supported by this adapter")
	}
	bin, err := p.ResolveBinary(ctx)
	if err != nil {
		return nil, err
	}
	cmd := []string{bin}
	appendApprovalFlags(&cmd, mode)
	agentbase.AppendModelFlag(&cmd, cfg, "--model")
	return cmd, nil
}

// appendApprovalFlags maps AO's permission modes onto GigaCode's
// `--approval-mode` choices (plan|default|auto-edit|auto). GigaCode has no
// "yolo" mode, so bypassPermissions maps to its most permissive mode, `auto`,
// whose server-side classifier still gates risky actions. Default emits no flag
// so GigaCode keeps the user's own configured mode.
func appendApprovalFlags(cmd *[]string, permissions ports.PermissionMode) {
	switch ports.NormalizePermissionMode(permissions) {
	case ports.PermissionModeAcceptEdits:
		*cmd = append(*cmd, "--approval-mode", "auto-edit")
	case ports.PermissionModeAuto, ports.PermissionModeBypassPermissions:
		*cmd = append(*cmd, "--approval-mode", "auto")
	}
}

// SessionInfo returns hook-derived native session metadata.
func (p *Plugin) SessionInfo(ctx context.Context, ref ports.SessionRef) (ports.SessionInfo, bool, error) {
	if err := ctx.Err(); err != nil {
		return ports.SessionInfo{}, false, err
	}
	info, ok := agentbase.StandardSessionInfo(ref)
	return info, ok, nil
}

// gigacodeBinarySpec lists where the `gigacode` launcher can live. The Windows
// installer drops `gigacode.cmd` under %LOCALAPPDATA%\Programs\gigacode-cli\bin
// (and adds it to PATH); the portable layout uses ~/.gigacode/bin.
var gigacodeBinarySpec = binaryutil.BinarySpec{
	Label:         "gigacode",
	Names:         []string{"gigacode"},
	WinNames:      []string{"gigacode.cmd", "gigacode.exe", "gigacode"},
	UnixPaths:     []string{"/usr/local/bin/gigacode", "/opt/homebrew/bin/gigacode"},
	UnixHomePaths: [][]string{{".gigacode", "bin", "gigacode"}, {".local", "bin", "gigacode"}},
	WinPaths: []binaryutil.WinPath{
		{Base: binaryutil.WinLocalAppData, Parts: []string{"Programs", "gigacode-cli", "bin", "gigacode.cmd"}},
		{Base: binaryutil.WinHome, Parts: []string{".gigacode", "bin", "gigacode.cmd"}},
	},
}

// ResolveBinary returns the path to the gigacode launcher, or a wrapped
// ports.ErrAgentBinaryNotFound when it is absent.
func (p *Plugin) ResolveBinary(ctx context.Context) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	p.binaryMu.Lock()
	defer p.binaryMu.Unlock()
	if p.resolvedBinary != "" {
		return p.resolvedBinary, nil
	}
	bin, err := binaryutil.ResolveBinary(ctx, gigacodeBinarySpec)
	if err != nil {
		return "", err
	}
	p.resolvedBinary = bin
	return bin, nil
}

// InvalidateBinaryResolution makes the next operation resolve GigaCode again,
// so a reinstall or upgrade that relocates the launcher is picked up.
func (p *Plugin) InvalidateBinaryResolution() {
	p.binaryMu.Lock()
	p.resolvedBinary = ""
	p.binaryMu.Unlock()
}
