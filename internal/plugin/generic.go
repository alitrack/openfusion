package plugin

import (
	"context"

	"github.com/lhy/openfusion/internal/types"
)

// GenericPlugin is a no-op plugin for providers that don't need optimization.
type GenericPlugin struct{}

func (p *GenericPlugin) Name() string { return "generic" }

func (p *GenericPlugin) TransformRequest(ctx context.Context, req *types.ChatRequest) (*types.ChatRequest, error) {
	return req, nil
}

func (p *GenericPlugin) TransformResponse(ctx context.Context, resp *types.ChatResponse) (*types.ChatResponse, error) {
	return resp, nil
}

func (p *GenericPlugin) Capabilities() Capabilities {
	return Capabilities{}
}
