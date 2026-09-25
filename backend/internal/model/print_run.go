package model

import "time"

// PrintRun models 印刷批次 as an independently versioned aggregate. The fields
// cover ownership, operational context, evidence and measured risk so later
// changes naturally span persistence, service and UI layers.
type PrintRun struct {
	BaseModel
	Facility    string             `json:"facility" gorm:"size:120;index"`
	Owner       string             `json:"owner" gorm:"size:120;index"`
	Category    string             `json:"category" gorm:"size:80;index"`
	RiskLevel   string             `json:"riskLevel" gorm:"size:32;index"`
	MetricValue float64            `json:"metricValue"`
	MetricUnit  string             `json:"metricUnit" gorm:"size:24"`
	EffectiveAt time.Time          `json:"effectiveAt"`
	Evidence    string             `json:"evidence" gorm:"size:2000"`
	RelatedCode string             `json:"relatedCode" gorm:"size:64;index"`
	Revisions   []PrintRunRevision `json:"revisions,omitempty" gorm:"foreignKey:PrintRunID"`
	Reworks     []RunRework        `json:"reworks,omitempty" gorm:"foreignKey:PrintRunID"`
}

func (item *PrintRun) GetBase() *BaseModel { return &item.BaseModel }

func (item PrintRun) TableName() string { return "print_runs" }

var PrintRunInitialStatus = "setup"

// PrintRunRevision is an append-only snapshot of the run's colour
// configuration. It is deliberately separate from optimistic locking so an
// operator can never overwrite the evidence used by an earlier decision.
type PrintRunRevision struct {
	ID          uint      `json:"id" gorm:"primaryKey"`
	PrintRunID  uint      `json:"printRunId" gorm:"not null;uniqueIndex:idx_print_run_revision"`
	Version     uint      `json:"version" gorm:"not null;uniqueIndex:idx_print_run_revision"`
	Status      string    `json:"status" gorm:"size:40;not null"`
	Name        string    `json:"name" gorm:"size:160;not null"`
	Facility    string    `json:"facility" gorm:"size:120"`
	Owner       string    `json:"owner" gorm:"size:120"`
	Category    string    `json:"category" gorm:"size:80"`
	RiskLevel   string    `json:"riskLevel" gorm:"size:32"`
	MetricValue float64   `json:"metricValue"`
	MetricUnit  string    `json:"metricUnit" gorm:"size:24"`
	Evidence    string    `json:"evidence" gorm:"size:2000"`
	RelatedCode string    `json:"relatedCode" gorm:"size:64"`
	Actor       string    `json:"actor" gorm:"size:80;not null"`
	RequestID   string    `json:"requestId" gorm:"size:80;not null"`
	Reason      string    `json:"reason" gorm:"size:500;not null"`
	CreatedAt   time.Time `json:"createdAt"`
}

// RunRework models 批次返修 initiated after a batch was already released. It
// replaces verbal-only colour-difference notifications: every post-release
// remediation carries a mandatory reason, waits for a replacement proof and is
// preserved forever as part of the batch history. Lifecycle: waiting ->
// released. Version provides optimistic locking against double-start/close.
type RunRework struct {
	BaseModel
	// PrintRunID / RunCode identify the released 印刷批次 being remediated.
	PrintRunID uint   `json:"printRunId" gorm:"not null;index"`
	RunCode    string `json:"runCode" gorm:"size:64;not null;index"`
	// ReworkCount is the 1-based cumulative rework sequence for the batch, so
	// "累计返修次数" survives across multiple release/rework cycles.
	ReworkCount uint `json:"reworkCount" gorm:"not null"`
	// Reason is the mandatory colour-difference explanation supplied by the
	// reviewer who initiates the rework.
	Reason string `json:"reason" gorm:"size:500;not null"`
	// StartedAt records 返修开始时间; CompletedAt is set when re-release lands.
	StartedAt   time.Time  `json:"startedAt" gorm:"not null"`
	CompletedAt *time.Time `json:"completedAt"`
	// ResolutionProofCode is the replacement proof that passed review and
	// unlocked re-release.
	ResolutionProofCode string `json:"resolutionProofCode" gorm:"size:64"`
	// InvalidatedProofs snapshots the proofs declared void when this rework
	// started; the underlying ColorProof rows move to invalidated.
	InvalidatedProofs []ColorProof `json:"invalidatedProofs,omitempty" gorm:"foreignKey:ReworkID"`
}

func (item RunRework) TableName() string { return "run_reworks" }

var RunReworkInitialStatus = "waiting"
