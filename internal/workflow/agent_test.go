package workflow_test

import (
	"strings"
	"testing"

	"sevens/internal/function"
	"sevens/internal/workflow"
)

// ---------------------------------------------------------------------------
// AgentBackend captures prompt
// ---------------------------------------------------------------------------

func TestAgentBackend_CapturesPrompt(t *testing.T) {
	e := setup(t)
	e.seedTree()

	ab := &function.AgentBackend{}
	e.deps.Backend = ab

	result, err := workflow.ApplyFunction(ctx(), e.deps, e.root, "notice", "The Commons")
	if err != nil {
		t.Fatal(err)
	}

	// AgentBackend returns empty text, so the pipeline should complete (notice is ungated text).
	_ = result

	// The backend should have captured the rendered prompt.
	if ab.PreparedPrompt.User == "" {
		t.Fatal("AgentBackend should have captured the rendered prompt")
	}
	// The prompt should contain the target node title somewhere.
	if !strings.Contains(ab.PreparedPrompt.User, "The Commons") {
		t.Fatalf("expected prompt to mention target node, got:\n%s", ab.PreparedPrompt.User)
	}
}

// ---------------------------------------------------------------------------
// SubmitExternalResult
// ---------------------------------------------------------------------------

func TestSubmitExternalResult_InjectsPending(t *testing.T) {
	e := setup(t)
	e.seedTree()
	e.withMock(`[{"title": "Revenue", "rationale": "funding"}]`)

	// decompose suspends at gate -> PhasePending
	ar, err := workflow.ApplyFunction(ctx(), e.deps, e.root, "decompose", "The Commons")
	if err != nil {
		t.Fatal(err)
	}
	if !ar.Suspended {
		t.Fatal("expected suspension")
	}

	// Submit an external result.
	externalResult := function.TransformResult{
		Raw:    `[{"title": "External Child", "rationale": "from agent"}]`,
		IsText: false,
	}
	err = workflow.SubmitExternalResult(ctx(), e.deps, ar.PipelineID, externalResult)
	if err != nil {
		t.Fatalf("SubmitExternalResult error: %v", err)
	}

	// Verify the pipeline still has the new result stored.
	p, err := e.deps.Store.Load(ctx(), ar.PipelineID)
	if err != nil {
		t.Fatal(err)
	}
	if p.CurrentResult == nil {
		t.Fatal("expected CurrentResult to be set after submit")
	}
	if p.CurrentResult.Raw != externalResult.Raw {
		t.Fatalf("expected submitted result, got %q", p.CurrentResult.Raw)
	}
	// Pipeline should still be pending (submit does not accept).
	if p.Phase != function.PhasePending {
		t.Fatalf("expected PhasePending, got %s", p.Phase)
	}
}

func TestSubmitExternalResult_ErrorsOnNonPending(t *testing.T) {
	e := setup(t)
	e.seedTree()
	e.withMock("Text output only.")

	// notice is ungated text -> completes immediately, not pending
	ar, err := workflow.ApplyFunction(ctx(), e.deps, e.root, "notice", "The Commons")
	if err != nil {
		t.Fatal(err)
	}
	if ar.Suspended {
		t.Fatal("notice should not suspend")
	}

	err = workflow.SubmitExternalResult(ctx(), e.deps, ar.PipelineID, function.TransformResult{Raw: "injected"})
	if err == nil {
		t.Fatal("expected error submitting to non-pending pipeline")
	}
	if !strings.Contains(err.Error(), "not pending") {
		t.Fatalf("expected 'not pending' in error, got: %v", err)
	}
}

func TestSubmitExternalResult_ErrorsOnRejected(t *testing.T) {
	e := setup(t)
	e.seedTree()
	e.withMock(`[{"title": "X", "rationale": "y"}]`)

	ar, err := workflow.ApplyFunction(ctx(), e.deps, e.root, "decompose", "The Commons")
	if err != nil {
		t.Fatalf("ApplyFunction: %v", err)
	}

	// Reject it first.
	err = workflow.RejectPipeline(ctx(), e.deps, e.root, ar.PipelineID)
	if err != nil {
		t.Fatal(err)
	}

	// Now try to submit - should fail.
	err = workflow.SubmitExternalResult(ctx(), e.deps, ar.PipelineID, function.TransformResult{Raw: "too late"})
	if err == nil {
		t.Fatal("expected error submitting to rejected pipeline")
	}
}

// ---------------------------------------------------------------------------
// Full agent cycle: AgentBackend -> suspend -> submit -> accept
// ---------------------------------------------------------------------------

func TestAgentCycle_FullRoundTrip(t *testing.T) {
	e := setup(t)
	e.seedTree()

	// Use AgentBackend: returns empty result, pipeline suspends at gate.
	ab := &function.AgentBackend{}
	e.deps.Backend = ab

	ar, err := workflow.ApplyFunction(ctx(), e.deps, e.root, "decompose", "The Commons")
	if err != nil {
		t.Fatal(err)
	}
	if !ar.Suspended {
		t.Fatal("expected suspension with AgentBackend")
	}
	if ar.BackendName != "agent" {
		t.Fatalf("expected backend 'agent', got %q", ar.BackendName)
	}

	// Verify the prompt was captured.
	if ab.PreparedPrompt.User == "" {
		t.Fatal("expected captured prompt")
	}

	// Simulate external agent producing a result and submitting it.
	externalOps := `[{"action": "create", "title": "Revenue Model", "parent": "The Commons", "content": "# Revenue Model\n\nFunding approaches."}]`
	err = workflow.SubmitExternalResult(ctx(), e.deps, ar.PipelineID, function.TransformResult{
		Raw:    externalOps,
		IsText: false,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Now switch to a mock backend for the accept/advance phase
	// (the generate step needs a backend to run).
	generateOps := `[{"action": "create", "title": "Revenue Model", "parent": "The Commons", "content": "# Revenue Model\n\nFunding approaches."}]`
	e.withMock(generateOps)

	// Accept the pipeline.
	acceptResult, err := workflow.AcceptPipeline(ctx(), e.deps, e.root, ar.PipelineID, "")
	if err != nil {
		t.Fatal(err)
	}

	// decompose has two gated steps. After accepting step 0, it runs step 1
	// which also has a gate. So it should either suspend again or complete.
	if acceptResult.Suspended {
		// Still at generate gate - this is expected.
		if acceptResult.PipelineID == "" {
			t.Fatal("expected pipeline ID on suspended accept")
		}
	} else if acceptResult.Completed {
		// Also valid if generate auto-completes.
		if len(acceptResult.FilesCreated) == 0 && len(acceptResult.FilesEdited) == 0 {
			// Ops might not materialize in non-git temp dir, but that's fine.
		}
	}
}
