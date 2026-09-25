package dto

import "time"

// CreatePrintRun is the public write contract for 印刷批次. Status is deliberately
// omitted so callers cannot bypass the service state machine.
type CreatePrintRun struct {
	Code        string    `json:"code" binding:"required,min=2,max=64"`
	Name        string    `json:"name" binding:"required,min=2,max=160"`
	Description string    `json:"description" binding:"max=1000"`
	Facility    string    `json:"facility" binding:"required,max=120"`
	Owner       string    `json:"owner" binding:"required,max=120"`
	Category    string    `json:"category" binding:"required,max=80"`
	RiskLevel   string    `json:"riskLevel" binding:"required,oneof=low medium high critical"`
	MetricValue float64   `json:"metricValue"`
	MetricUnit  string    `json:"metricUnit" binding:"max=24"`
	EffectiveAt time.Time `json:"effectiveAt" binding:"required"`
	Evidence    string    `json:"evidence" binding:"max=2000"`
	RelatedCode string    `json:"relatedCode" binding:"max=64"`
}

type UpdatePrintRun struct {
	ExpectedVersion uint      `json:"expectedVersion" binding:"required"`
	Name            string    `json:"name" binding:"required,min=2,max=160"`
	Description     string    `json:"description" binding:"max=1000"`
	Facility        string    `json:"facility" binding:"required,max=120"`
	Owner           string    `json:"owner" binding:"required,max=120"`
	Category        string    `json:"category" binding:"required,max=80"`
	RiskLevel       string    `json:"riskLevel" binding:"required,oneof=low medium high critical"`
	MetricValue     float64   `json:"metricValue"`
	MetricUnit      string    `json:"metricUnit" binding:"max=24"`
	EffectiveAt     time.Time `json:"effectiveAt" binding:"required"`
	Evidence        string    `json:"evidence" binding:"max=2000"`
	RelatedCode     string    `json:"relatedCode" binding:"max=64"`
}

// ReworkRequest pulls a released batch back into hold. Reason is validated in
// the service layer so a missing reason surfaces as a 409 conflict rather
// than a generic binding error.
type ReworkRequest struct {
	ExpectedVersion uint   `json:"expectedVersion" binding:"required"`
	Reason          string `json:"reason" binding:"max=500"`
}

// ReworkCondition is one gate the batch must satisfy before it may be
// released again after a rework cycle.
type ReworkCondition struct {
	Label string `json:"label"`
	Met   bool   `json:"met"`
}

// ReworkProof is the read-side summary of a 校样 invalidated by the rework.
type ReworkProof struct {
	ID          uint      `json:"id"`
	Code        string    `json:"code"`
	Name        string    `json:"name"`
	Status      string    `json:"status"`
	MetricValue float64   `json:"metricValue"`
	MetricUnit  string    `json:"metricUnit"`
	Evidence    string    `json:"evidence"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// ReworkInfo powers the batch detail view: when the rework started, which
// proofs were invalidated and what must happen before re-release.
type ReworkInfo struct {
	RunID               uint              `json:"runId"`
	RunCode             string            `json:"runCode"`
	Status              string            `json:"status"`
	ReworkStartedAt     *time.Time        `json:"reworkStartedAt"`
	ReworkCount         uint              `json:"reworkCount"`
	InvalidatedProofs   []ReworkProof     `json:"invalidatedProofs"`
	ReReleaseReady      bool              `json:"reReleaseReady"`
	ReReleaseConditions []ReworkCondition `json:"reReleaseConditions"`
}
