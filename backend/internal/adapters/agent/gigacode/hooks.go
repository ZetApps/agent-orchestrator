package gigacode

import (
	"context"
	"path/filepath"

	"github.com/aoagents/agent-orchestrator/backend/internal/adapters/agent/hooksjson"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

const (
	// hookCommandPrefix identifies the hook commands AO owns, so install skips
	// duplicates and uninstall recognizes AO entries by prefix.
	hookCommandPrefix = "ao hooks gigacode "

	// hookTimeout is in seconds: GigaCode treats hook timeouts below 1000 as
	// seconds and only accepts larger values as legacy milliseconds.
	hookTimeout = 30
)

// gigacodeHooks manages AO's hooks in the workspace-local
// .gigacode/settings.json file. The events use GigaCode's Claude-Code-shaped
// names; SessionStart carries the native session id AO needs for resume.
var gigacodeHooks = hooksjson.Manager{
	Label:         "gigacode",
	CommandPrefix: hookCommandPrefix,
	Timeout:       hookTimeout,
	Path:          func(ws string) string { return filepath.Join(ws, ".gigacode", "settings.json") },
	Managed: []hooksjson.HookSpec{
		{Event: "SessionStart", Command: hookCommandPrefix + "session-start"},
		{Event: "UserPromptSubmit", Command: hookCommandPrefix + "user-prompt-submit"},
		{Event: "PermissionRequest", Command: hookCommandPrefix + "permission-request"},
		{Event: "Stop", Command: hookCommandPrefix + "stop"},
	},
}

// GetAgentHooks installs AO's GigaCode hooks, preserving user-defined hooks and
// unrelated settings.
func (p *Plugin) GetAgentHooks(ctx context.Context, cfg ports.WorkspaceHookConfig) error {
	return gigacodeHooks.Install(ctx, cfg.WorkspacePath)
}

// UninstallHooks removes AO's GigaCode hooks, leaving user-defined hooks untouched.
func (p *Plugin) UninstallHooks(ctx context.Context, workspacePath string) error {
	return gigacodeHooks.Uninstall(ctx, workspacePath)
}

// AreHooksInstalled reports whether any AO GigaCode hook is present.
func (p *Plugin) AreHooksInstalled(ctx context.Context, workspacePath string) (bool, error) {
	return gigacodeHooks.AreInstalled(ctx, workspacePath)
}
