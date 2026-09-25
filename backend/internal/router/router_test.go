package router_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/blueship581/print-color-calibration-release/backend/internal/config"
	"github.com/blueship581/print-color-calibration-release/backend/internal/database"
	"github.com/blueship581/print-color-calibration-release/backend/internal/router"
	"github.com/gin-gonic/gin"
)

type apiEnvelope struct {
	Data json.RawMessage `json:"data"`
}

func TestRBACAndImmutableRevisionFlows(t *testing.T) {
	cfg := testConfig(filepath.Join(t.TempDir(), "gb517.db"))
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	db, redisClient, err := database.Open(context.Background(), cfg, logger)
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	engine := router.New(cfg, db, redisClient, logger)
	tokens := map[string]string{}
	for _, role := range []string{"viewer", "operator", "reviewer", "admin"} {
		tokens[role] = loginToken(t, engine, role)
	}

	payload := recordPayload("RD-TEST-001", "测试放行决定")
	if status, _ := perform(t, engine, http.MethodPost, "/api/release", tokens["viewer"], "viewer-create", payload); status != http.StatusForbidden {
		t.Fatalf("viewer create status = %d, want 403", status)
	}
	status, body := perform(t, engine, http.MethodPost, "/api/release", tokens["operator"], "decision-create", payload)
	if status != http.StatusCreated {
		t.Fatalf("operator create decision status = %d body=%s", status, body)
	}
	decision := decodeData[struct {
		ID      uint `json:"id"`
		Version uint `json:"version"`
	}](t, body)
	transition := map[string]any{"status": "release", "expectedVersion": decision.Version, "reason": "quality gate accepted"}
	path := "/api/release/" + uintString(decision.ID) + "/transition"
	if status, _ := perform(t, engine, http.MethodPost, path, tokens["operator"], "operator-release", transition); status != http.StatusForbidden {
		t.Fatalf("operator release status = %d, want 403", status)
	}
	status, body = perform(t, engine, http.MethodPost, path, tokens["reviewer"], "reviewer-release", transition)
	if status != http.StatusOK {
		t.Fatalf("reviewer release status = %d body=%s", status, body)
	}
	status, body = perform(t, engine, http.MethodGet, "/api/release/"+uintString(decision.ID), tokens["reviewer"], "decision-read", nil)
	detail := decodeData[struct {
		Version   uint `json:"version"`
		Revisions []struct {
			Version   uint   `json:"version"`
			RequestID string `json:"requestId"`
		} `json:"revisions"`
	}](t, body)
	if status != http.StatusOK || detail.Version != 2 || len(detail.Revisions) != 2 || detail.Revisions[0].RequestID != "reviewer-release" {
		t.Fatalf("unexpected decision revision chain: status=%d detail=%+v", status, detail)
	}
	update := recordPayload("ignored", "不得覆盖的决定")
	update["expectedVersion"] = detail.Version
	if status, _ := perform(t, engine, http.MethodPut, "/api/release/"+uintString(decision.ID), tokens["operator"], "locked-update", update); status != http.StatusConflict {
		t.Fatalf("resolved decision update status = %d, want 409", status)
	}
	if status, _ := perform(t, engine, http.MethodDelete, "/api/release/"+uintString(decision.ID), tokens["admin"], "locked-delete", nil); status != http.StatusConflict {
		t.Fatalf("resolved decision delete status = %d, want 409", status)
	}

	runPayload := recordPayload("PR-TEST-001", "测试色彩配置")
	status, body = perform(t, engine, http.MethodPost, "/api/runs", tokens["operator"], "run-create", runPayload)
	run := decodeData[struct {
		ID      uint `json:"id"`
		Version uint `json:"version"`
	}](t, body)
	if status != http.StatusCreated {
		t.Fatalf("create run status = %d body=%s", status, body)
	}
	runPath := "/api/runs/" + uintString(run.ID) + "/transition"
	status, _ = perform(t, engine, http.MethodPost, runPath, tokens["operator"], "run-printing", map[string]any{"status": "printing", "expectedVersion": run.Version, "reason": "plates and ink verified"})
	if status != http.StatusOK {
		t.Fatalf("run transition status = %d", status)
	}
	if status, _ := perform(t, engine, http.MethodDelete, "/api/runs/"+uintString(run.ID), tokens["admin"], "locked-run-delete", nil); status != http.StatusConflict {
		t.Fatalf("active run delete status = %d, want 409", status)
	}
	_, body = perform(t, engine, http.MethodGet, "/api/runs/"+uintString(run.ID), tokens["operator"], "run-read", nil)
	runDetail := decodeData[struct {
		Revisions []struct {
			RequestID string `json:"requestId"`
		} `json:"revisions"`
	}](t, body)
	if len(runDetail.Revisions) != 2 || runDetail.Revisions[0].RequestID != "run-printing" {
		t.Fatalf("unexpected colour configuration revisions: %+v", runDetail.Revisions)
	}

	if status, _ := perform(t, engine, http.MethodGet, "/api/audits", tokens["viewer"], "viewer-audit", nil); status != http.StatusForbidden {
		t.Fatalf("viewer audit status = %d, want 403", status)
	}
	if status, _ := perform(t, engine, http.MethodGet, "/api/audits", tokens["reviewer"], "reviewer-audit", nil); status != http.StatusOK {
		t.Fatalf("reviewer audit status = %d, want 200", status)
	}
}

func TestBatchReworkFlow(t *testing.T) {
	cfg := testConfig(filepath.Join(t.TempDir(), "gb517-rework.db"))
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	db, redisClient, err := database.Open(context.Background(), cfg, logger)
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	engine := router.New(cfg, db, redisClient, logger)
	tokens := map[string]string{}
	for _, role := range []string{"operator", "reviewer"} {
		tokens[role] = loginToken(t, engine, role)
	}

	// A released batch with an accepted proof linked by relatedCode.
	status, body := perform(t, engine, http.MethodPost, "/api/runs", tokens["operator"], "rework-run-create", recordPayload("PR-REWORK-001", "返修测试批次"))
	if status != http.StatusCreated {
		t.Fatalf("create run status = %d body=%s", status, body)
	}
	run := decodeData[struct {
		ID      uint `json:"id"`
		Version uint `json:"version"`
	}](t, body)
	runTransitions := []string{"printing", "proofing", "released"}
	for _, target := range runTransitions {
		transition := map[string]any{"status": target, "expectedVersion": run.Version, "reason": "rework flow setup"}
		actor := tokens["operator"]
		if target == "released" {
			actor = tokens["reviewer"]
		}
		status, body = perform(t, engine, http.MethodPost, "/api/runs/"+uintString(run.ID)+"/transition", actor, "run-"+target, transition)
		if status != http.StatusOK {
			t.Fatalf("run -> %s status = %d body=%s", target, status, body)
		}
		run.Version++
	}
	proofPayload := recordPayload("CP-REWORK-001", "返修前校样")
	proofPayload["relatedCode"] = "PR-REWORK-001"
	status, body = perform(t, engine, http.MethodPost, "/api/proofs", tokens["operator"], "old-proof-create", proofPayload)
	if status != http.StatusCreated {
		t.Fatalf("create proof status = %d body=%s", status, body)
	}
	oldProof := decodeData[struct {
		ID      uint `json:"id"`
		Version uint `json:"version"`
	}](t, body)
	for _, step := range []struct {
		target, requestID, token string
	}{
		{"review", "old-proof-review", tokens["operator"]},
		{"accepted", "old-proof-accept", tokens["reviewer"]},
	} {
		status, body = perform(t, engine, http.MethodPost, "/api/proofs/"+uintString(oldProof.ID)+"/transition", step.token, step.requestID,
			map[string]any{"status": step.target, "expectedVersion": oldProof.Version, "reason": "proof baseline"})
		if status != http.StatusOK {
			t.Fatalf("proof -> %s status = %d body=%s", step.target, status, body)
		}
		oldProof.Version++
	}

	reworkPath := "/api/runs/" + uintString(run.ID) + "/rework"
	// Only reviewers may initiate a rework.
	if status, _ := perform(t, engine, http.MethodPost, reworkPath, tokens["operator"], "rework-operator", map[string]any{"expectedVersion": run.Version, "reason": "色差超标"}); status != http.StatusForbidden {
		t.Fatalf("operator rework status = %d, want 403", status)
	}
	// Missing reason, stale version and wrong state are all 409 conflicts.
	if status, _ := perform(t, engine, http.MethodPost, reworkPath, tokens["reviewer"], "rework-no-reason", map[string]any{"expectedVersion": run.Version}); status != http.StatusConflict {
		t.Fatalf("rework without reason status = %d, want 409", status)
	}
	if status, _ := perform(t, engine, http.MethodPost, reworkPath, tokens["reviewer"], "rework-stale", map[string]any{"expectedVersion": run.Version + 9, "reason": "色差超标"}); status != http.StatusConflict {
		t.Fatalf("rework with stale version status = %d, want 409", status)
	}
	// Failed attempts must not mutate the batch or its version chain.
	_, body = perform(t, engine, http.MethodGet, "/api/runs/"+uintString(run.ID), tokens["reviewer"], "run-after-conflicts", nil)
	untouched := decodeData[struct {
		Status   string `json:"status"`
		Version  uint   `json:"version"`
		Revision []struct {
			Version uint `json:"version"`
		} `json:"revisions"`
	}](t, body)
	if untouched.Status != "released" || untouched.Version != run.Version || len(untouched.Revision) != 4 {
		t.Fatalf("conflicts must preserve the released batch and version chain: %+v", untouched)
	}

	// Reviewer files the reason and initiates the rework.
	status, body = perform(t, engine, http.MethodPost, reworkPath, tokens["reviewer"], "rework-filed", map[string]any{"expectedVersion": run.Version, "reason": "放行后抽检发现色差 ΔE 超标"})
	if status != http.StatusOK {
		t.Fatalf("reviewer rework status = %d body=%s", status, body)
	}
	reworked := decodeData[struct {
		Status          string     `json:"status"`
		Version         uint       `json:"version"`
		ReworkCount     uint       `json:"reworkCount"`
		ReworkStartedAt *time.Time `json:"reworkStartedAt"`
	}](t, body)
	if reworked.Status != "hold" || reworked.ReworkCount != 1 || reworked.ReworkStartedAt == nil || reworked.Version != run.Version+1 {
		t.Fatalf("unexpected reworked batch: %+v", reworked)
	}
	run.Version = reworked.Version
	// A batch already in hold cannot be reworked again.
	if status, _ := perform(t, engine, http.MethodPost, reworkPath, tokens["reviewer"], "rework-again", map[string]any{"expectedVersion": run.Version, "reason": "重复返修"}); status != http.StatusConflict {
		t.Fatalf("rework on hold batch status = %d, want 409", status)
	}
	// The earlier proof is now invalid and the detail exposes it.
	_, body = perform(t, engine, http.MethodGet, "/api/proofs/"+uintString(oldProof.ID), tokens["reviewer"], "old-proof-read", nil)
	invalidProof := decodeData[struct {
		Status string `json:"status"`
	}](t, body)
	if invalidProof.Status != "invalid" {
		t.Fatalf("old proof status = %s, want invalid", invalidProof.Status)
	}
	_, body = perform(t, engine, http.MethodGet, reworkPath, tokens["reviewer"], "rework-detail", nil)
	detail := decodeData[struct {
		ReworkStartedAt   *time.Time `json:"reworkStartedAt"`
		ReworkCount       uint       `json:"reworkCount"`
		InvalidatedProofs []struct {
			Code   string `json:"code"`
			Status string `json:"status"`
		} `json:"invalidatedProofs"`
		ReReleaseReady      bool `json:"reReleaseReady"`
		ReReleaseConditions []struct {
			Label string `json:"label"`
			Met   bool   `json:"met"`
		} `json:"reReleaseConditions"`
	}](t, body)
	if detail.ReworkStartedAt == nil || detail.ReworkCount != 1 || len(detail.InvalidatedProofs) != 1 ||
		detail.InvalidatedProofs[0].Code != "CP-REWORK-001" || detail.InvalidatedProofs[0].Status != "invalid" ||
		detail.ReReleaseReady || len(detail.ReReleaseConditions) != 4 || !detail.ReReleaseConditions[0].Met {
		t.Fatalf("unexpected rework detail: %+v", detail)
	}

	// The batch cannot be released again before a fresh proof passes review.
	status, body = perform(t, engine, http.MethodPost, "/api/runs/"+uintString(run.ID)+"/transition", tokens["operator"], "run-back-proofing",
		map[string]any{"status": "proofing", "expectedVersion": run.Version, "reason": "返修后重新校样"})
	if status != http.StatusOK {
		t.Fatalf("run hold -> proofing status = %d body=%s", status, body)
	}
	run.Version++
	if status, _ := perform(t, engine, http.MethodPost, "/api/runs/"+uintString(run.ID)+"/transition", tokens["reviewer"], "run-rerelease-blocked",
		map[string]any{"status": "released", "expectedVersion": run.Version, "reason": "未补做校样"}); status != http.StatusConflict {
		t.Fatalf("re-release without fresh proof status = %d, want 409", status)
	}

	// A fresh proof for the same batch passes review and unlocks re-release.
	newProofPayload := recordPayload("CP-REWORK-002", "返修后补做校样")
	newProofPayload["relatedCode"] = "PR-REWORK-001"
	status, body = perform(t, engine, http.MethodPost, "/api/proofs", tokens["operator"], "new-proof-create", newProofPayload)
	if status != http.StatusCreated {
		t.Fatalf("create replacement proof status = %d body=%s", status, body)
	}
	newProof := decodeData[struct {
		ID      uint `json:"id"`
		Version uint `json:"version"`
	}](t, body)
	for _, step := range []struct {
		target, requestID, token string
	}{
		{"review", "new-proof-review", tokens["operator"]},
		{"accepted", "new-proof-accept", tokens["reviewer"]},
	} {
		status, body = perform(t, engine, http.MethodPost, "/api/proofs/"+uintString(newProof.ID)+"/transition", step.token, step.requestID,
			map[string]any{"status": step.target, "expectedVersion": newProof.Version, "reason": "返修补做校样"})
		if status != http.StatusOK {
			t.Fatalf("new proof -> %s status = %d body=%s", step.target, status, body)
		}
		newProof.Version++
	}
	status, body = perform(t, engine, http.MethodPost, "/api/runs/"+uintString(run.ID)+"/transition", tokens["reviewer"], "run-rereleased",
		map[string]any{"status": "released", "expectedVersion": run.Version, "reason": "补做校样已通过复核"})
	if status != http.StatusOK {
		t.Fatalf("re-release after accepted proof status = %d body=%s", status, body)
	}
	rereleased := decodeData[struct {
		Status      string `json:"status"`
		ReworkCount uint   `json:"reworkCount"`
	}](t, body)
	if rereleased.Status != "released" || rereleased.ReworkCount != 1 {
		t.Fatalf("unexpected re-released batch: %+v", rereleased)
	}
}

func testConfig(dsn string) config.Config {
	return config.Config{
		AppName: "print-color-calibration-release", Environment: "test", Port: "0",
		DatabaseDriver: "sqlite", DatabaseDSN: dsn, JWTSecret: "gb517-router-tests-secret",
		TokenTTL: time.Hour, RequestLimit: 1000, StartupTimeout: time.Second,
		ShutdownTimeout: time.Second, ReadHeaderTimeout: time.Second, ReadTimeout: time.Second,
		WriteTimeout: time.Second, IdleTimeout: time.Second,
	}
}

func loginToken(t *testing.T, engine *gin.Engine, username string) string {
	t.Helper()
	status, body := perform(t, engine, http.MethodPost, "/api/auth/login", "", "login-"+username, map[string]any{"username": username, "password": "Admin123!"})
	if status != http.StatusOK {
		t.Fatalf("login %s status = %d body=%s", username, status, body)
	}
	return decodeData[struct {
		Token string `json:"token"`
	}](t, body).Token
}

func recordPayload(code, name string) map[string]any {
	return map[string]any{
		"code": code, "name": name, "description": "router integration test",
		"facility": "测试印刷区", "owner": "operator", "category": "校准",
		"riskLevel": "medium", "metricValue": 2.1, "metricUnit": "dE",
		"effectiveAt": time.Now().UTC().Format(time.RFC3339), "evidence": "spectrophotometer evidence", "relatedCode": "PR-001",
	}
}

func perform(t *testing.T, engine *gin.Engine, method, path, token, requestID string, payload any) (int, []byte) {
	t.Helper()
	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("marshal payload: %v", err)
		}
		body = bytes.NewReader(encoded)
	}
	request := httptest.NewRequest(method, path, body)
	request.Header.Set("X-Request-ID", requestID)
	if payload != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	return response.Code, response.Body.Bytes()
}

func decodeData[T any](t *testing.T, body []byte) T {
	t.Helper()
	var envelope apiEnvelope
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("decode envelope %s: %v", body, err)
	}
	var value T
	if err := json.Unmarshal(envelope.Data, &value); err != nil {
		t.Fatalf("decode data %s: %v", envelope.Data, err)
	}
	return value
}

func uintString(value uint) string {
	return strconv.FormatUint(uint64(value), 10)
}
