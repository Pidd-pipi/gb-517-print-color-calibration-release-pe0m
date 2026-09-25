package router_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/blueship581/print-color-calibration-release/backend/internal/config"
	"github.com/blueship581/print-color-calibration-release/backend/internal/database"
	"github.com/blueship581/print-color-calibration-release/backend/internal/router"
	"github.com/gin-gonic/gin"
)

var reworkSeq uint64

func reworkEngine(t *testing.T) (*gin.Engine, map[string]string) {
	t.Helper()
	cfg := testConfig(filepath.Join(t.TempDir(), "gb517-rework.db"))
	logger := discardTestLogger()
	db, redisClient, err := database.Open(context.Background(), cfg, logger)
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	engine := router.New(cfg, db, redisClient, logger)
	tokens := map[string]string{}
	for _, role := range []string{"viewer", "operator", "reviewer", "admin"} {
		tokens[role] = loginToken(t, engine, role)
	}
	return engine, tokens
}

func discardTestLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func timestampCode() string {
	n := atomic.AddUint64(&reworkSeq, 1)
	return time.Now().Format("150405") + "-" + strconv.FormatUint(n, 10)
}

type runRef struct {
	id      uint
	version uint
	code    string
}

// createReleasedRun drives a new batch setup -> printing -> proofing ->
// released and returns its identifiers.
func createReleasedRun(t *testing.T, engine *gin.Engine, tokens map[string]string) runRef {
	t.Helper()
	code := "PR-RW-" + timestampCode()
	payload := recordPayload(code, "返修流程测试批次")
	status, body := perform(t, engine, http.MethodPost, "/api/runs", tokens["operator"], "rw-run-create", payload)
	if status != http.StatusCreated {
		t.Fatalf("create run status = %d body=%s", status, body)
	}
	run := decodeData[struct {
		ID      uint   `json:"id"`
		Version uint   `json:"version"`
		Code    string `json:"code"`
	}](t, body)
	ref := runRef{id: run.ID, version: run.Version, code: run.Code}
	ref.version = moveRun(t, engine, tokens["operator"], ref, "printing", "rw-run-printing", "上机印刷核对完成")
	ref.version = moveRun(t, engine, tokens["operator"], ref, "proofing", "rw-run-proofing", "开始抽取校样")
	ref.version = moveRun(t, engine, tokens["reviewer"], ref, "released", "rw-run-released", "校样合格，批次放行")
	return ref
}

func moveRun(t *testing.T, engine *gin.Engine, token string, ref runRef, status, requestID, reason string) uint {
	t.Helper()
	code, body := perform(t, engine, http.MethodPost, "/api/runs/"+uintString(ref.id)+"/transition", token, requestID,
		map[string]any{"status": status, "expectedVersion": ref.version, "reason": reason})
	if code != http.StatusOK {
		t.Fatalf("run -> %s status = %d body=%s", status, code, body)
	}
	return decodeData[struct {
		Version uint `json:"version"`
	}](t, body).Version
}

type proofRef struct {
	id      uint
	version uint
	code    string
}

// createAcceptedProof captures a proof bound to runCode, submits it to review
// and has the reviewer accept it.
func createAcceptedProof(t *testing.T, engine *gin.Engine, tokens map[string]string, runCode string) proofRef {
	t.Helper()
	code := "CP-RW-" + timestampCode()
	payload := recordPayload(code, "返修补做校样 "+runCode)
	payload["runCode"] = runCode
	payload["relatedCode"] = "RW-TEST"
	payload["metricValue"] = 1.4
	status, body := perform(t, engine, http.MethodPost, "/api/proofs", tokens["operator"], "rw-proof-create", payload)
	if status != http.StatusCreated {
		t.Fatalf("create proof status = %d body=%s", status, body)
	}
	proof := decodeData[struct {
		ID      uint   `json:"id"`
		Version uint   `json:"version"`
		Code    string `json:"code"`
	}](t, body)
	ref := proofRef{id: proof.ID, version: proof.Version, code: proof.Code}
	ref.version = moveProof(t, engine, tokens["operator"], ref, "review", "rw-proof-review", "校样读数采集完成")
	ref.version = moveProof(t, engine, tokens["reviewer"], ref, "accepted", "rw-proof-accept", "色差在容差内，复核通过")
	return ref
}

func moveProof(t *testing.T, engine *gin.Engine, token string, ref proofRef, status, requestID, reason string) uint {
	t.Helper()
	code, body := perform(t, engine, http.MethodPost, "/api/proofs/"+uintString(ref.id)+"/transition", token, requestID,
		map[string]any{"status": status, "expectedVersion": ref.version, "reason": reason})
	if code != http.StatusOK {
		t.Fatalf("proof -> %s status = %d body=%s", status, code, body)
	}
	return decodeData[struct {
		Version uint `json:"version"`
	}](t, body).Version
}

var _ = config.Config{}
