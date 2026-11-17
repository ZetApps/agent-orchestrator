package gigacode

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

// fakeBinary puts an executable named gigacode first on PATH and returns it.
func fakeBinary(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	name := "gigacode"
	if runtime.GOOS == "windows" {
		name = "gigacode.cmd"
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("@echo off\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	return path
}

func TestLaunchCommand(t *testing.T) {
	bin := fakeBinary(t)
	cases := []struct {
		name string
		cfg  ports.LaunchConfig
		want []string
	}{
		{"default has no flags", ports.LaunchConfig{}, []string{bin}},
		{"prompt stays interactive", ports.LaunchConfig{Prompt: "-fix it"}, []string{bin, "--prompt-interactive", "-fix it"}},
		{"accept edits", ports.LaunchConfig{Permissions: ports.PermissionModeAcceptEdits}, []string{bin, "--approval-mode", "auto-edit"}},
		{"auto", ports.LaunchConfig{Permissions: ports.PermissionModeAuto}, []string{bin, "--approval-mode", "auto"}},
		{"bypass maps to auto", ports.LaunchConfig{Permissions: ports.PermissionModeBypassPermissions}, []string{bin, "--approval-mode", "auto"}},
		{"model", ports.LaunchConfig{Config: ports.AgentConfig{Model: " GigaCode-Pro "}}, []string{bin, "--model", "GigaCode-Pro"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := New().GetLaunchCommand(context.Background(), tc.cfg)
			if err != nil {
				t.Fatalf("GetLaunchCommand: %v", err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("argv = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestLaunchRejectsToolRestrictions(t *testing.T) {
	fakeBinary(t)
	_, err := New().GetLaunchCommand(context.Background(), ports.LaunchConfig{AllowedTools: []string{"read"}})
	if err == nil || !strings.Contains(err.Error(), "tool restrictions") {
		t.Fatalf("err = %v, want tool restrictions error", err)
	}
}

func TestBinaryNotFound(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", t.TempDir())
	t.Setenv("LOCALAPPDATA", t.TempDir())
	if _, err := New().GetLaunchCommand(context.Background(), ports.LaunchConfig{}); err == nil {
		t.Fatal("expected missing binary error")
	}
}

func TestRestoreCommand(t *testing.T) {
	bin := fakeBinary(t)
	p := New()
	session := ports.SessionRef{Metadata: map[string]string{ports.MetadataKeyAgentSessionID: " abc-123 "}}

	got, ok, err := p.GetRestoreCommand(context.Background(), ports.RestoreConfig{Session: session})
	if err != nil || !ok {
		t.Fatalf("GetRestoreCommand = (%v, %v)", ok, err)
	}
	if want := []string{bin, "--resume", "abc-123"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("argv = %q, want %q", got, want)
	}

	_, ok, err = p.GetRestoreCommand(context.Background(), ports.RestoreConfig{})
	if err != nil || ok {
		t.Fatalf("without native id = (%v, %v), want not restorable", ok, err)
	}
}

func TestHooksInstallPreservesUserSettings(t *testing.T) {
	ctx := context.Background()
	ws := t.TempDir()
	path := filepath.Join(ws, ".gigacode", "settings.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"theme":"dark"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	p := New()

	if err := p.GetAgentHooks(ctx, ports.WorkspaceHookConfig{WorkspacePath: ws}); err != nil {
		t.Fatalf("install: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, want := range []string{`"theme"`, "ao hooks gigacode session-start", "ao hooks gigacode user-prompt-submit", "ao hooks gigacode permission-request", "ao hooks gigacode stop"} {
		if !strings.Contains(text, want) {
			t.Fatalf("settings missing %q:\n%s", want, text)
		}
	}
	if installed, err := p.AreHooksInstalled(ctx, ws); err != nil || !installed {
		t.Fatalf("AreHooksInstalled = (%v, %v)", installed, err)
	}

	if err := p.UninstallHooks(ctx, ws); err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	if installed, _ := p.AreHooksInstalled(ctx, ws); installed {
		t.Fatal("hooks still installed after uninstall")
	}
	if data, _ := os.ReadFile(path); !strings.Contains(string(data), `"theme"`) {
		t.Fatalf("uninstall dropped user settings:\n%s", data)
	}
}
