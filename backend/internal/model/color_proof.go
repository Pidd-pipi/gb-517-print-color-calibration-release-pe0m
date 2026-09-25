package model

import "time"

// ColorProof models 色彩校样 as an independently versioned aggregate. The fields
// cover ownership, operational context, evidence and measured risk so later
// changes naturally span persistence, service and UI layers.
type ColorProof struct {
	BaseModel
	Facility    string    `json:"facility" gorm:"size:120;index"`
	Owner       string    `json:"owner" gorm:"size:120;index"`
	Category    string    `json:"category" gorm:"size:80;index"`
	RiskLevel   string    `json:"riskLevel" gorm:"size:32;index"`
	MetricValue float64   `json:"metricValue"`
	MetricUnit  string    `json:"metricUnit" gorm:"size:24"`
	EffectiveAt time.Time `json:"effectiveAt"`
	Evidence    string    `json:"evidence" gorm:"size:2000"`
	RelatedCode string    `json:"relatedCode" gorm:"size:64;index"`
	// RunCode binds the proof to a concrete 印刷批次 (PrintRun.Code). Proofs that
	// pre-date the rework feature may leave it empty. When a batch enters
	// rework its proofs are invalidated and replacement proofs must carry the
	// same run code to unlock re-release.
	RunCode string `json:"runCode" gorm:"size:64;index"`
	// ReworkID is set when the proof is invalidated by or created to resolve a
	// 批次返修. It stays zero for proofs outside any rework cycle.
	ReworkID uint `json:"reworkId" gorm:"index"`
}

func (item *ColorProof) GetBase() *BaseModel { return &item.BaseModel }

func (item ColorProof) TableName() string { return "color_proofs" }

var ColorProofInitialStatus = "captured"
