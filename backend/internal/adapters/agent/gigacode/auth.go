package gigacode

import (
	"context"

	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

var _ ports.AgentAuthChecker = (*Plugin)(nil)

// AuthStatus reports whether the CLI is installed. GigaCode signs in through its
// own interactive `/auth` flow and keeps device-bound credentials AO must not
// read, so a signed-in state cannot be established locally: an installed CLI is
// reported as unknown rather than guessed authorized or unauthorized.
func (p *Plugin) AuthStatus(ctx context.Context) (ports.AgentAuthStatus, error) {
	if _, err := p.ResolveBinary(ctx); err != nil {
		return ports.AgentAuthStatusUnknown, err
	}
	return ports.AgentAuthStatusUnknown, nil
}
