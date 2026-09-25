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
	Rework(context.Context, uint, dto.ReworkRequest, string, string, string) (model.PrintRun, error)
	ReworkInfo(context.Context, uint) (dto.ReworkInfo, error)
	Delete(context.Context, uint, string, string) error
	StatusCounts(context.Context) (map[string]int64, error)
}

type printRunService struct {
	repository repository.PrintRunRepository
	proofs     repository.ColorProofRepository
	security   SecurityService
}

func NewPrintRunService(repo repository.PrintRunRepository, proofs repository.ColorProofRepository, security SecurityService) PrintRunService {
	return &printRunService{repository: repo, proofs: proofs, security: security}
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
	if (target == string(constants.RunStateReleased) || current.Status == string(constants.RunStateReleased)) && !canReview(role) {
		return model.PrintRun{}, ErrForbidden
	}
	if !constants.CanTransition(constants.PrintRunTransitions, current.Status, target) {
		return model.PrintRun{}, fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, current.Status, target)
	}
	if target == string(constants.RunStateReleased) && current.ReworkCount > 0 {
		if err := s.ensureRereleaseReady(ctx, current); err != nil {
			return model.PrintRun{}, err
		}
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

// Rework pulls a released batch back into hold after a colour deviation was
// discovered post-release. Only a reviewer may initiate it, a reason is
// mandatory, and the run must still be released at the expected version;
// every violation is a 409 conflict that leaves the batch, its 放行决定 and
// the version chain untouched.
func (s *printRunService) Rework(ctx context.Context, id uint, input dto.ReworkRequest, actor, role, requestID string) (model.PrintRun, error) {
	if !canReview(role) {
		return model.PrintRun{}, ErrForbidden
	}
	current, err := s.repository.Get(ctx, id)
	if err != nil {
		return model.PrintRun{}, err
	}
	reason := strings.TrimSpace(input.Reason)
	if reason == "" {
		return model.PrintRun{}, fmt.Errorf("%w: 返修原因缺失", ErrReworkConflict)
	}
	if current.Status != string(constants.RunStateReleased) {
		return model.PrintRun{}, fmt.Errorf("%w: 批次状态为 %s，仅已放行批次可返修", ErrReworkConflict, current.Status)
	}
	now := time.Now().UTC()
	current.Status = string(constants.RunStateHold)
	current.ReworkStartedAt = &now
	current.ReworkCount++
	current.Version = input.ExpectedVersion + 1
	current.UpdatedAt = now
	invalidated, err := s.repository.ApplyRework(ctx, id, input.ExpectedVersion, &current, actor, requestID, reason)
	if err != nil {
		return model.PrintRun{}, fmt.Errorf("rework 印刷批次: %w", err)
	}
	detail := fmt.Sprintf("%s（失效校样 %d 份，累计返修 %d 次）", reason, invalidated, current.ReworkCount)
	if err := s.security.Audit(ctx, actor, requestID, "rework", "PrintRun", id, string(constants.RunStateReleased), string(constants.RunStateHold), detail); err != nil {
		return model.PrintRun{}, fmt.Errorf("persist rework audit: %w", err)
	}
	return s.repository.Get(ctx, id)
}

// ReworkInfo assembles the batch detail view: when the current rework cycle
// started, which 校样 it invalidated and the exact conditions that must be
// met before the batch may be released again.
func (s *printRunService) ReworkInfo(ctx context.Context, id uint) (dto.ReworkInfo, error) {
	current, err := s.repository.Get(ctx, id)
	if err != nil {
		return dto.ReworkInfo{}, err
	}
	info := dto.ReworkInfo{
		RunID: current.ID, RunCode: current.Code, Status: current.Status,
		ReworkStartedAt: current.ReworkStartedAt, ReworkCount: current.ReworkCount,
		InvalidatedProofs: []dto.ReworkProof{}, ReReleaseConditions: []dto.ReworkCondition{},
	}
	proofs, err := s.proofs.ListByRelatedCode(ctx, current.Code)
	if err != nil {
		return dto.ReworkInfo{}, fmt.Errorf("list 校样 for rework detail: %w", err)
	}
	newProofCaptured := false
	for _, proof := range proofs {
		if proof.Status == constants.ProofStateInvalid {
			info.InvalidatedProofs = append(info.InvalidatedProofs, dto.ReworkProof{
				ID: proof.ID, Code: proof.Code, Name: proof.Name, Status: proof.Status,
				MetricValue: proof.MetricValue, MetricUnit: proof.MetricUnit,
				Evidence: proof.Evidence, UpdatedAt: proof.UpdatedAt,
			})
			continue
		}
		if current.ReworkStartedAt != nil && !proof.CreatedAt.Before(*current.ReworkStartedAt) {
			newProofCaptured = true
		}
	}
	accepted := int64(0)
	if current.ReworkStartedAt != nil {
		accepted, err = s.proofs.CountAcceptedRelatedSince(ctx, current.Code, *current.ReworkStartedAt)
		if err != nil {
			return dto.ReworkInfo{}, fmt.Errorf("count accepted 校样: %w", err)
		}
	}
	proofAccepted := accepted > 0
	releasableState := current.Status == string(constants.RunStateProofing)
	info.ReReleaseConditions = []dto.ReworkCondition{
		{Label: "复核员已填写原因并发起返修，批次进入等待状态", Met: current.ReworkCount > 0},
		{Label: "已为同一批次补做新校样", Met: newProofCaptured},
		{Label: "补做校样已通过复核（accepted）", Met: proofAccepted},
		{Label: "批次已回到校样中状态，可由复核员再次放行", Met: releasableState},
	}
	info.ReReleaseReady = newProofCaptured && proofAccepted && releasableState
	return info, nil
}

// ensureRereleaseReady blocks re-release of a reworked batch until a fresh
// 校样 for the same batch has been accepted after the rework started.
func (s *printRunService) ensureRereleaseReady(ctx context.Context, current model.PrintRun) error {
	if current.ReworkStartedAt == nil {
		return fmt.Errorf("%w: 返修批次缺少开始时间", ErrReworkConflict)
	}
	accepted, err := s.proofs.CountAcceptedRelatedSince(ctx, current.Code, *current.ReworkStartedAt)
	if err != nil {
		return fmt.Errorf("count accepted 校样: %w", err)
	}
	if accepted == 0 {
		return fmt.Errorf("%w: 补做校样未通过复核，批次不可再次放行", ErrReworkConflict)
	}
	return nil
}

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
