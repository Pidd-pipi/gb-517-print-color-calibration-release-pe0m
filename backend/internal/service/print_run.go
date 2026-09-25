package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/blueship581/print-color-calibration-release/backend/internal/constants"
	"github.com/blueship581/print-color-calibration-release/backend/internal/dto"
	"github.com/blueship581/print-color-calibration-release/backend/internal/model"
	"github.com/blueship581/print-color-calibration-release/backend/internal/repository"
)

type PrintRunService interface {
	List(context.Context, dto.PageQuery) (repository.Page[model.PrintRun], error)
	Get(context.Context, uint) (model.PrintRun, error)
	Create(context.Context, dto.CreatePrintRun, string, string) (model.PrintRun, error)
	Update(context.Context, uint, dto.UpdatePrintRun, string, string) (model.PrintRun, error)
	Transition(context.Context, uint, dto.TransitionRequest, string, string, string) (model.PrintRun, error)
	Delete(context.Context, uint, string, string) error
	StatusCounts(context.Context) (map[string]int64, error)
}

type printRunService struct {
	repository repository.PrintRunRepository
	security   SecurityService
	rework     RunReworkService
}

func NewPrintRunService(repo repository.PrintRunRepository, security SecurityService, rework RunReworkService) PrintRunService {
	return &printRunService{repository: repo, security: security, rework: rework}
}

func (s *printRunService) List(ctx context.Context, query dto.PageQuery) (repository.Page[model.PrintRun], error) {
	return s.repository.List(ctx, query)
}

func (s *printRunService) Get(ctx context.Context, id uint) (model.PrintRun, error) {
	return s.repository.Get(ctx, id)
}

func (s *printRunService) Create(ctx context.Context, input dto.CreatePrintRun, actor, requestID string) (model.PrintRun, error) {
	if err := validatePrintRunBusinessFields(input.Code, input.Name, input.Facility, input.Owner); err != nil {
		return model.PrintRun{}, err
	}
	item := model.PrintRun{
		BaseModel: model.BaseModel{
			Code: strings.ToUpper(strings.TrimSpace(input.Code)), Name: strings.TrimSpace(input.Name),
			Status: model.PrintRunInitialStatus, Version: 1, Description: strings.TrimSpace(input.Description),
		},
		Facility: strings.TrimSpace(input.Facility), Owner: strings.TrimSpace(input.Owner),
		Category: strings.TrimSpace(input.Category), RiskLevel: input.RiskLevel,
		MetricValue: input.MetricValue, MetricUnit: strings.TrimSpace(input.MetricUnit),
		EffectiveAt: input.EffectiveAt.UTC(), Evidence: strings.TrimSpace(input.Evidence),
		RelatedCode: strings.ToUpper(strings.TrimSpace(input.RelatedCode)),
	}
	if err := s.repository.CreateVersioned(ctx, &item, actor, requestID, "created colour configuration"); err != nil {
		return model.PrintRun{}, fmt.Errorf("create 印刷批次: %w", err)
	}
	_ = s.security.Audit(ctx, actor, requestID, "create", "PrintRun", item.ID, "", item.Status, "created 印刷批次")
	return item, nil
}

func (s *printRunService) Update(ctx context.Context, id uint, input dto.UpdatePrintRun, actor, requestID string) (model.PrintRun, error) {
	current, err := s.repository.Get(ctx, id)
	if err != nil {
		return model.PrintRun{}, err
	}
	if err := validatePrintRunBusinessFields(current.Code, input.Name, input.Facility, input.Owner); err != nil {
		return model.PrintRun{}, err
	}
	current.Name = strings.TrimSpace(input.Name)
	current.Description = strings.TrimSpace(input.Description)
	current.Facility = strings.TrimSpace(input.Facility)
	current.Owner = strings.TrimSpace(input.Owner)
	current.Category = strings.TrimSpace(input.Category)
	current.RiskLevel = input.RiskLevel
	current.MetricValue = input.MetricValue
	current.MetricUnit = strings.TrimSpace(input.MetricUnit)
	current.EffectiveAt = input.EffectiveAt.UTC()
	current.Evidence = strings.TrimSpace(input.Evidence)
	current.RelatedCode = strings.ToUpper(strings.TrimSpace(input.RelatedCode))
	current.Version = input.ExpectedVersion + 1
	current.UpdatedAt = time.Now().UTC()
	if err := s.repository.UpdateVersioned(ctx, id, input.ExpectedVersion, &current, actor, requestID, "updated colour configuration"); err != nil {
		return model.PrintRun{}, fmt.Errorf("update 印刷批次: %w", err)
	}
	_ = s.security.Audit(ctx, actor, requestID, "update", "PrintRun", id, current.Status, current.Status, "updated business fields")
	return s.repository.Get(ctx, id)
}

func (s *printRunService) Transition(ctx context.Context, id uint, input dto.TransitionRequest, actor, role, requestID string) (model.PrintRun, error) {
	current, err := s.repository.Get(ctx, id)
	if err != nil {
		return model.PrintRun{}, err
	}
	target := strings.TrimSpace(input.Status)
	if target == string(constants.RunStateReleased) && !canReview(role) {
		return model.PrintRun{}, ErrForbidden
	}
	// A batch waiting in 批次返修 can only return to released through the gated
	// re-release: a replacement proof for the same batch must have passed
	// review since the rework started.
	if current.Status == string(constants.RunStateReworkPending) && target == string(constants.RunStateReleased) {
		if err := s.rework.CloseForRelease(ctx, id, input.ExpectedVersion, actor, requestID, strings.TrimSpace(input.Reason)); err != nil {
			return model.PrintRun{}, err
		}
		return s.repository.Get(ctx, id)
	}
	if current.Status == string(constants.RunStateReleased) && !canReview(role) {
		return model.PrintRun{}, ErrForbidden
	}
	if !constants.CanTransition(constants.PrintRunTransitions, current.Status, target) {
		return model.PrintRun{}, fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, current.Status, target)
	}
	before := current.Status
	current.Status = target
	current.Version = input.ExpectedVersion + 1
	current.UpdatedAt = time.Now().UTC()
	if err := s.repository.UpdateVersioned(ctx, id, input.ExpectedVersion, &current, actor, requestID, input.Reason); err != nil {
		return model.PrintRun{}, fmt.Errorf("transition 印刷批次: %w", err)
	}
	if err := s.security.Audit(ctx, actor, requestID, "transition", "PrintRun", id, before, target, input.Reason); err != nil {
		return model.PrintRun{}, fmt.Errorf("persist transition audit: %w", err)
	}
	return s.repository.Get(ctx, id)
}

func canReview(role string) bool { return role == model.RoleReviewer || role == model.RoleAdmin }

func (s *printRunService) Delete(ctx context.Context, id uint, actor, requestID string) error {
	current, err := s.repository.Get(ctx, id)
	if err != nil {
		return err
	}
	if current.Status != model.PrintRunInitialStatus {
		return ErrLocked
	}
	if err := s.repository.Delete(ctx, id); err != nil {
		return err
	}
	return s.security.Audit(ctx, actor, requestID, "delete", "PrintRun", id, current.Status, "deleted", "soft deleted 印刷批次")
}

func (s *printRunService) StatusCounts(ctx context.Context) (map[string]int64, error) {
	return s.repository.CountByStatus(ctx)
}

func validatePrintRunBusinessFields(code, name, facility, owner string) error {
	if strings.TrimSpace(code) == "" || strings.TrimSpace(name) == "" || strings.TrimSpace(facility) == "" || strings.TrimSpace(owner) == "" {
		return ErrInvalidInput
	}
	return nil
}

// ReworkDetail is the read model for 批次返修详情: start time, the proofs that
// became void and the exact condition under which re-release is allowed.
type ReworkDetail struct {
	model.RunRework
	// ReleaseReady is true only when a replacement proof captured after the
	// rework started has passed reviewer acceptance (or the rework completed).
	ReleaseReady bool `json:"releaseReady"`
	// ReleaseCondition explains 再次放行条件 in human-readable form.
	ReleaseCondition string `json:"releaseCondition"`
	// AcceptedProof is the qualifying replacement proof, when present.
	AcceptedProof *model.ColorProof `json:"acceptedProof,omitempty"`
}

type RunReworkService interface {
	List(context.Context, dto.ReworkListQuery) (repository.Page[model.RunRework], error)
	Get(context.Context, uint) (ReworkDetail, error)
	OpenForRun(context.Context, uint) (model.RunRework, bool, error)
	Start(context.Context, uint, dto.StartReworkRequest, string, string, string) (model.RunRework, error)
	// CloseForRelease performs the gated re-release of a waiting 批次返修.
	CloseForRelease(ctx context.Context, runID, expectedVersion uint, actor, requestID, reason string) error
}

type runReworkService struct {
	reworks  repository.RunReworkRepository
	runs     repository.PrintRunRepository
	proofs   ColorProofService
	security SecurityService
}

func NewRunReworkService(reworks repository.RunReworkRepository, runs repository.PrintRunRepository, proofs ColorProofService, security SecurityService) RunReworkService {
	return &runReworkService{reworks: reworks, runs: runs, proofs: proofs, security: security}
}

func (s *runReworkService) List(ctx context.Context, query dto.ReworkListQuery) (repository.Page[model.RunRework], error) {
	return s.reworks.List(ctx, query)
}

func (s *runReworkService) detailFrom(ctx context.Context, rework model.RunRework) ReworkDetail {
	detail := ReworkDetail{RunRework: rework,
		ReleaseCondition: "为同一批次补做校样并通过质量复核后，批次才可再次放行"}
	if rework.Status == "released" && rework.ResolutionProofCode != "" {
		// Loaded by code so the detail stays correct even after a later rework
		// cycle invalidates the proof that originally closed this one.
		if proof, ok, err := s.proofs.GetByCode(ctx, rework.ResolutionProofCode); err == nil && ok {
			proofCopy := proof
			detail.AcceptedProof = &proofCopy
		}
		detail.ReleaseReady = true
		return detail
	}
	if proof, ok, err := s.proofs.LatestAcceptedForRun(ctx, rework.RunCode, rework.StartedAt); err == nil && ok {
		proofCopy := proof
		detail.AcceptedProof = &proofCopy
		detail.ReleaseReady = true
	}
	return detail
}

func (s *runReworkService) Get(ctx context.Context, id uint) (ReworkDetail, error) {
	rework, err := s.reworks.Get(ctx, id)
	if err != nil {
		return ReworkDetail{}, err
	}
	return s.detailFrom(ctx, rework), nil
}

func (s *runReworkService) OpenForRun(ctx context.Context, runID uint) (model.RunRework, bool, error) {
	return s.reworks.OpenForRun(ctx, runID)
}

func (s *runReworkService) Start(ctx context.Context, runID uint, input dto.StartReworkRequest, actor, role, requestID string) (model.RunRework, error) {
	if !canReview(role) {
		return model.RunRework{}, ErrForbidden
	}
	// 原因缺失 -> 冲突（与状态不符、版本变化一致地返回 409，而不是 400）。
	if strings.TrimSpace(input.Reason) == "" {
		return model.RunRework{}, fmt.Errorf("%w: rework reason is required", ErrReworkConflict)
	}
	if len(strings.TrimSpace(input.Reason)) < 3 {
		return model.RunRework{}, fmt.Errorf("%w: rework reason is too short", ErrReworkConflict)
	}
	run, err := s.runs.Get(ctx, runID)
	if err != nil {
		return model.RunRework{}, err
	}
	// 状态不符 -> 冲突：只有已放行批次可以补返修，且不能重复发起。
	if run.Status != string(constants.RunStateReleased) {
		return model.RunRework{}, fmt.Errorf("%w: run is %s, only released runs can enter rework", ErrReworkConflict, run.Status)
	}
	if open, exists, err := s.reworks.OpenForRun(ctx, runID); err != nil {
		return model.RunRework{}, err
	} else if exists {
		return model.RunRework{}, fmt.Errorf("%w: waiting rework %s already exists", ErrReworkConflict, open.Code)
	}
	now := time.Now().UTC()
	rework, err := s.reworks.Start(ctx, &run, input.ExpectedVersion, strings.TrimSpace(input.Reason), actor, requestID, now)
	if err != nil {
		return model.RunRework{}, fmt.Errorf("start 批次返修: %w", err)
	}
	if err := s.security.Audit(ctx, actor, requestID, "rework_start", "PrintRun", runID,
		string(constants.RunStateReleased), string(constants.RunStateReworkPending),
		"批次返修 #"+fmt.Sprint(rework.ReworkCount)+": "+strings.TrimSpace(input.Reason)); err != nil {
		return model.RunRework{}, err
	}
	if err := s.security.Audit(ctx, actor, requestID, "rework_start", "RunRework", rework.ID,
		"", "waiting", "started post-release rework"); err != nil {
		return model.RunRework{}, err
	}
	return rework, nil
}

func (s *runReworkService) CloseForRelease(ctx context.Context, runID, expectedVersion uint, actor, requestID, reason string) error {
	run, err := s.runs.Get(ctx, runID)
	if err != nil {
		return err
	}
	if run.Status != string(constants.RunStateReworkPending) {
		return fmt.Errorf("%w: run is %s, expected rework_pending", ErrReworkConflict, run.Status)
	}
	rework, exists, err := s.reworks.OpenForRun(ctx, runID)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("%w: no waiting rework exists for run %s", ErrReworkConflict, run.Code)
	}
	// 再次放行条件：同一批次、返修开始后补做并通过复核的校样。
	proof, hasProof, err := s.proofs.LatestAcceptedForRun(ctx, run.Code, rework.StartedAt)
	if err != nil {
		return err
	}
	if !hasProof {
		return fmt.Errorf("%w: replacement proof for %s must be captured and accepted before re-release", ErrReworkConflict, run.Code)
	}
	now := time.Now().UTC()
	closeReason := reason
	if strings.TrimSpace(closeReason) == "" {
		closeReason = "released after replacement proof " + proof.Code + " accepted"
	}
	if err := s.reworks.Complete(ctx, &run, expectedVersion, &rework, proof.Code, actor, requestID, closeReason, now); err != nil {
		return fmt.Errorf("re-release 印刷批次: %w", err)
	}
	if err := s.security.Audit(ctx, actor, requestID, "rework_release", "PrintRun", runID,
		string(constants.RunStateReworkPending), string(constants.RunStateReleased),
		"返修批次再次放行，依据补做校样 "+proof.Code); err != nil {
		return err
	}
	return s.security.Audit(ctx, actor, requestID, "rework_release", "RunRework", rework.ID,
		"waiting", "released", "closed by re-release with proof "+proof.Code)
}
