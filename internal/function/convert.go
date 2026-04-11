package function

import (
	"sevens/internal/apply"
)

// ConvertFunction converts an apply.Function (EDN-loaded) to a function.Function.
func ConvertFunction(old *apply.Function) *Function {
	fn := &Function{
		Name:        old.Name,
		Description: old.Description,
	}

	// Context policy from agent config
	if old.Agent != nil && old.Agent.ContextPolicy != "" {
		fn.ContextPolicy = old.Agent.ContextPolicy
	}

	oldSteps := old.EffectiveSteps()
	for _, os := range oldSteps {
		step := convertStep(os, old)
		fn.Steps = append(fn.Steps, step)
	}

	return fn
}

func convertStep(old apply.Step, fn *apply.Function) Step {
	step := Step{
		Name:       old.Name,
		ComposedOf: old.Fn,
		MapOver:    old.MapOver,
	}

	// Convert requires
	for _, r := range old.Requires {
		step.Requires = append(step.Requires, Require{
			Role:     r.Role,
			Type:     r.Type,
			Optional: r.Optional,
			As:       r.As,
		})
	}
	// Add function-level requires
	for _, r := range fn.Requires {
		step.Requires = append(step.Requires, Require{
			Role:     r.Role,
			Type:     r.Type,
			Optional: r.Optional,
			As:       r.As,
		})
	}

	// Convert context paths
	for _, p := range fn.Context {
		step.Paths = append(step.Paths, PathSpec{
			Path:        p.Path,
			ExcludeSelf: p.ExcludeSelf,
			With:        p.With,
			As:          p.As,
		})
	}

	// Convert output shape
	switch old.Output {
	case "ops":
		step.Output = Signature{Shape: ShapeFileOps}
	case "suggestions":
		step.Output = Signature{Shape: ShapeStructured}
	default:
		step.Output = Signature{Shape: ShapeText}
	}

	// Convert input
	switch old.Input {
	case "ops":
		step.Input = Signature{Shape: ShapeFileOps}
	case "suggestions":
		step.Input = Signature{Shape: ShapeStructured}
	default:
		step.Input = Signature{Shape: ShapeText}
	}

	// Convert gate
	if old.Gate == "approve" {
		step.Gate = &GateSpec{
			Revisable:  true,
			Cancelable: true,
		}
	}

	// Convert backend spec
	step.Backend = BackendSpec{
		Kind:           BackendLLM,
		PromptTemplate: old.Prompt,
	}
	agent := apply.EffectiveAgent(fn, &old)
	if agent != nil {
		step.Backend.Persona = agent.Persona
		step.Backend.SystemPrompt = agent.SystemPrompt
	}

	return step
}
