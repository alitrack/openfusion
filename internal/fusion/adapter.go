package fusion

import "github.com/lhy/openfusion/internal/types"

// AdaptPresetToTopology converts a legacy types.Preset (flat panel + judge)
// to a multi-layer TopologyDef. Backward-compatible: existing preset YAML
// files work unchanged with the new topology engine.
func AdaptPresetToTopology(p *types.Preset) *TopologyDef {
	panelModels := make([]ModelRef, len(p.Panel))
	for i, m := range p.Panel {
		panelModels[i] = ModelRef{
			Provider: m.Provider,
			Model:    m.Model,
			Role:     "panel",
		}
	}

	judgeModels := []ModelRef{{
		Provider: p.Judge.Provider,
		Model:    p.Judge.Model,
		Role:     "judge",
	}}

	return &TopologyDef{
		Layers: []LayerDef{
			{Name: "panel", Models: panelModels},
			{Name: "judge", Models: judgeModels, Role: "judge"},
		},
	}
}
