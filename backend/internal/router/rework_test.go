package router_test

import (
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
)

// TestPostReleaseReworkFlow verifies 批次返修: a reviewer recalls an already
// released batch (waiting state, start time, cumulative count, prior proofs
// invalidated), and the batch can only be re-released after a replacement
// proof for the same batch passes review. Missing reason, wrong state and
// stale versions all return 409; the original revision chain is preserved.
func TestPostReleaseReworkFlow(t *testing.T) {
	engine, tokens := reworkEngine(t)

	// --- A fully released batch with an accepted proof -----------------------
	run := createReleasedRun(t, engine, tokens)
	proof := createAcceptedProof(t, engine, tokens, run.code)
	reworkPath := "/api/runs/" + uintString(run.id) + "/rework"

	// Starting rework is reviewer-only and reason-required.
	start := map[string]any{"expectedVersion": run.version, "reason": "放行后抽检发现色差 ΔE 超标"}
	if status, _ := perform(t, engine, http.MethodPost, reworkPath, tokens["operator"], "rework-deny", start); status != http.StatusForbidden {
		t.Fatalf("operator start rework status = %d, want 403", status)
	}
	if status, body := perform(t, engine, http.MethodPost, reworkPath, tokens["reviewer"], "rework-no-reason",
		map[string]any{"expectedVersion": run.version, "reason": "   "}); status != http.StatusConflict {
		t.Fatalf("missing reason status = %d body=%s, want 409", status, body)
	}
	if status, _ := perform(t, engine, http.MethodPost, reworkPath, tokens["reviewer"], "rework-stale",
		map[string]any{"expectedVersion": run.version + 99, "reason": "版本已变化的返修"}); status != http.StatusConflict {
		t.Fatalf("stale version start status = %d, want 409", status)
	}

	status, body := perform(t, engine, http.MethodPost, reworkPath, tokens["reviewer"], "rework-start-1", start)
	if status != http.StatusCreated {
		t.Fatalf("start rework status = %d body=%s", status, body)
	}
	rework := decodeData[struct {
		ID          uint   `json:"id"`
		Status      string `json:"status"`
		PrintRunID  uint   `json:"printRunId"`
		ReworkCount uint   `json:"reworkCount"`
		StartedAt   string `json:"startedAt"`
		RunCode     string `json:"runCode"`
	}](t, body)
	if rework.Status != "waiting" || rework.ReworkCount != 1 || rework.StartedAt == "" || rework.PrintRunID != run.id {
		t.Fatalf("unexpected rework record: %+v", rework)
	}

	// Batch waits, version advanced, immutable revision chain appended.
	_, body = perform(t, engine, http.MethodGet, "/api/runs/"+uintString(run.id), tokens["reviewer"], "run-waiting", nil)
	runAfter := decodeData[struct {
		Status    string `json:"status"`
		Version   uint   `json:"version"`
		Revisions []struct {
			RequestID string `json:"requestId"`
		} `json:"revisions"`
	}](t, body)
	if runAfter.Status != "rework_pending" || runAfter.Version != run.version+1 {
		t.Fatalf("run after rework = %+v, want rework_pending v%d", runAfter, run.version+1)
	}
	if len(runAfter.Revisions) != 5 || runAfter.Revisions[0].RequestID != "rework-start-1" {
		t.Fatalf("rework revision not appended to immutable chain: %+v", runAfter.Revisions)
	}

	// Prior accepted proof is now invalidated and frozen.
	_, body = perform(t, engine, http.MethodGet, "/api/proofs/"+uintString(proof.id), tokens["reviewer"], "proof-void", nil)
	if got := decodeData[struct {
		Status string `json:"status"`
	}](t, body).Status; got != "invalidated" {
		t.Fatalf("prior proof status = %s, want invalidated", got)
	}
	if status, _ := perform(t, engine, http.MethodPost, "/api/proofs/"+uintString(proof.id)+"/transition", tokens["reviewer"], "void-move",
		map[string]any{"status": "review", "expectedVersion": proof.version + 1, "reason": "失效校样不得再流转"}); status != http.StatusConflict {
		t.Fatalf("invalidated proof transition status = %d, want 409", status)
	}

	// Duplicate start while waiting conflicts.
	if status, _ := perform(t, engine, http.MethodPost, reworkPath, tokens["reviewer"], "rework-dup",
		map[string]any{"expectedVersion": runAfter.Version, "reason": "重复返修应被拒绝"}); status != http.StatusConflict {
		t.Fatalf("duplicate rework status = %d, want 409", status)
	}

	// Re-release blocked until a same-batch replacement proof is accepted.
	if status, _ := perform(t, engine, http.MethodPost, "/api/runs/"+uintString(run.id)+"/transition", tokens["reviewer"], "release-too-early",
		map[string]any{"status": "released", "expectedVersion": runAfter.Version, "reason": "尚无补做校样"}); status != http.StatusConflict {
		t.Fatalf("premature re-release status = %d, want 409", status)
	}
	replacement := createAcceptedProof(t, engine, tokens, run.code)

	// Operator still cannot cross the gate; reviewer re-release closes rework.
	if status, _ := perform(t, engine, http.MethodPost, "/api/runs/"+uintString(run.id)+"/transition", tokens["operator"], "release-op-deny",
		map[string]any{"status": "released", "expectedVersion": runAfter.Version, "reason": "operator cannot re-release"}); status != http.StatusForbidden {
		t.Fatalf("operator re-release status = %d, want 403", status)
	}
	status, body = perform(t, engine, http.MethodPost, "/api/runs/"+uintString(run.id)+"/transition", tokens["reviewer"], "rework-release-1",
		map[string]any{"status": "released", "expectedVersion": runAfter.Version, "reason": "补做校样通过复核，恢复放行"})
	if status != http.StatusOK {
		t.Fatalf("re-release status = %d body=%s", status, body)
	}
	released := decodeData[struct {
		Status  string `json:"status"`
		Version uint   `json:"version"`
	}](t, body)
	if released.Status != "released" || released.Version != runAfter.Version+1 {
		t.Fatalf("re-released run = %+v", released)
	}

	// Detail: start time, completed time, invalidated proof, accepted proof and
	// the human-readable re-release condition.
	status, body = perform(t, engine, http.MethodGet, "/api/reworks/"+uintString(rework.ID), tokens["reviewer"], "rework-detail", nil)
	detail := decodeData[struct {
		Status           string `json:"status"`
		StartedAt        string `json:"startedAt"`
		CompletedAt      string `json:"completedAt"`
		ReleaseReady     bool   `json:"releaseReady"`
		ReleaseCondition string `json:"releaseCondition"`
		ResolutionProof  struct {
			Code string `json:"code"`
		} `json:"acceptedProof"`
		Invalidated []struct {
			ID     uint   `json:"id"`
			Status string `json:"status"`
		} `json:"invalidatedProofs"`
	}](t, body)
	if status != http.StatusOK || detail.Status != "released" || detail.StartedAt == "" || detail.CompletedAt == "" {
		t.Fatalf("unexpected rework detail: status=%d %+v", status, detail)
	}
	if !detail.ReleaseReady || detail.ReleaseCondition == "" {
		t.Fatalf("rework detail missing release readiness/condition: %+v", detail)
	}
	if len(detail.Invalidated) != 1 || detail.Invalidated[0].ID != proof.id {
		t.Fatalf("invalidated proofs = %+v, want only %d", detail.Invalidated, proof.id)
	}
	if detail.ResolutionProof.Code != replacement.code {
		t.Fatalf("resolution proof = %s, want %s", detail.ResolutionProof.Code, replacement.code)
	}

	// A second cycle on the re-released batch accumulates the count.
	status, body = perform(t, engine, http.MethodPost, reworkPath, tokens["reviewer"], "rework-start-2",
		map[string]any{"expectedVersion": released.Version, "reason": "再次发现批次色差需要返修"})
	if status != http.StatusCreated {
		t.Fatalf("second cycle start status = %d body=%s", status, body)
	}
	if count := decodeData[struct {
		ReworkCount uint `json:"reworkCount"`
	}](t, body).ReworkCount; count != 2 {
		t.Fatalf("cumulative rework count = %d, want 2", count)
	}

	// Starting rework on a batch that was never released conflicts (状态不符).
	other := createRunOnly(t, engine, tokens)
	if status, _ := perform(t, engine, http.MethodPost, "/api/runs/"+uintString(other.id)+"/rework", tokens["reviewer"], "rework-wrong-state",
		map[string]any{"expectedVersion": other.version, "reason": "未放行批次不能返修"}); status != http.StatusConflict {
		t.Fatalf("non-released rework start status = %d, want 409", status)
	}
}

// createRunOnly returns a freshly created batch still in setup.
func createRunOnly(t *testing.T, engine *gin.Engine, tokens map[string]string) runRef {
	t.Helper()
	code := "PR-RW-NEW-" + timestampCode()
	payload := recordPayload(code, "未放行批次")
	status, body := perform(t, engine, http.MethodPost, "/api/runs", tokens["operator"], "rw-other-create", payload)
	if status != http.StatusCreated {
		t.Fatalf("create other run status = %d body=%s", status, body)
	}
	run := decodeData[struct {
		ID      uint   `json:"id"`
		Version uint   `json:"version"`
		Code    string `json:"code"`
	}](t, body)
	return runRef{id: run.ID, version: run.Version, code: run.Code}
}
