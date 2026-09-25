package constants

// Shared status values are mirrored in frontend/src/types/status.ts. Keeping
// the lists explicit makes state-machine drift visible during code review.

type RunState string

const (
	RunStateSetup         RunState = "setup"
	RunStatePrinting      RunState = "printing"
	RunStateProofing      RunState = "proofing"
	RunStateHold          RunState = "hold"
	RunStateReleased      RunState = "released"
	RunStateReworkPending RunState = "rework_pending"
)

var AllRunState = []string{"setup", "printing", "proofing", "hold", "released", "rework_pending"}

type DecisionType string

const (
	DecisionTypeRelease    DecisionType = "release"
	DecisionTypeRework     DecisionType = "rework"
	DecisionTypeQuarantine DecisionType = "quarantine"
)

var AllDecisionType = []string{"release", "rework", "quarantine"}

// ProofInvalidated marks 校样 invalidated by a post-release 批次返修. It is a
// terminal state and is only reachable through the rework workflow, never via
// the generic transition endpoint.
const ProofInvalidated = "invalidated"

// Rework lifecycle: waiting while the shop floor remediates the colour
// difference, then released once a freshly captured proof passes review.
const (
	ReworkStatusWaiting  = "waiting"
	ReworkStatusReleased = "released"
)

var PressUnitTransitions = map[string]map[string]bool{
	"ready":       {"setup": true, "printing": true},
	"setup":       {"printing": true, "maintenance": true, "ready": true},
	"printing":    {"maintenance": true, "setup": true},
	"maintenance": {"printing": true},
}

// released has no outgoing edge on purpose: post-release remediation must go
// through StartRework (released -> rework_pending), which also records the
// reason, invalidates prior proofs and appends a revision. While waiting the
// batch has no generic moves either — replacement proofs are captured as
// separate records, and the only exit is the proof-gated re-release.
var PrintRunTransitions = map[string]map[string]bool{
	"setup":          {"printing": true},
	"printing":       {"proofing": true, "hold": true, "setup": true},
	"proofing":       {"hold": true, "released": true, "printing": true},
	"hold":           {"proofing": true},
	"rework_pending": {"released": true},
}

var ColorProofTransitions = map[string]map[string]bool{
	"captured":    {"review": true},
	"review":      {"accepted": true, "rejected": true, "captured": true},
	"accepted":    {"review": true},
	"rejected":    {"review": true},
	"invalidated": {},
}

var ReleaseDecisionTransitions = map[string]map[string]bool{
	"draft":      {"release": true, "rework": true},
	"release":    {"rework": true, "quarantine": true, "draft": true},
	"rework":     {"quarantine": true, "release": true},
	"quarantine": {"rework": true},
}

func CanTransition(graph map[string]map[string]bool, from, to string) bool {
	targets, exists := graph[from]
	return exists && targets[to]
}
