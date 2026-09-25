package repository

import (
	"context"

	"github.com/blueship581/print-color-calibration-release/backend/internal/constants"
	"github.com/blueship581/print-color-calibration-release/backend/internal/dto"
	"github.com/blueship581/print-color-calibration-release/backend/internal/model"
	"gorm.io/gorm"
)

// PrintRunRepository owns all persistence operations for 印刷批次.
type PrintRunRepository interface {
	List(context.Context, dto.PageQuery) (Page[model.PrintRun], error)
	Get(context.Context, uint) (model.PrintRun, error)
	CreateVersioned(context.Context, *model.PrintRun, string, string, string) error
	UpdateVersioned(context.Context, uint, uint, *model.PrintRun, string, string, string) error
	ApplyRework(context.Context, uint, uint, *model.PrintRun, string, string, string) (int64, error)
	Delete(context.Context, uint) error
	CountByStatus(context.Context) (map[string]int64, error)
}

type printRunRepository struct {
	store *Store[model.PrintRun]
}

func NewPrintRunRepository(db *gorm.DB) PrintRunRepository {
	return &printRunRepository{store: NewStore[model.PrintRun](db)}
}

func (r *printRunRepository) List(ctx context.Context, q dto.PageQuery) (Page[model.PrintRun], error) {
	return r.store.List(ctx, q)
}
func (r *printRunRepository) Get(ctx context.Context, id uint) (model.PrintRun, error) {
	var item model.PrintRun
	err := r.store.db.WithContext(ctx).
		Preload("Revisions", func(db *gorm.DB) *gorm.DB { return db.Order("version DESC") }).
		First(&item, id).Error
	return item, err
}
func (r *printRunRepository) CreateVersioned(ctx context.Context, item *model.PrintRun, actor, requestID, reason string) error {
	return r.store.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Omit("Revisions").Create(item).Error; err != nil {
			return err
		}
		return tx.Create(printRunRevision(item, actor, requestID, reason)).Error
	})
}
func (r *printRunRepository) UpdateVersioned(ctx context.Context, id, version uint, item *model.PrintRun, actor, requestID, reason string) error {
	return r.store.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&model.PrintRun{}).Where("id = ? AND version = ?", id, version).
			Select("*").Omit("id", "code", "created_at", "deleted_at", "Revisions").Updates(item)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return ErrVersionConflict
		}
		return tx.Create(printRunRevision(item, actor, requestID, reason)).Error
	})
}

func printRunRevision(item *model.PrintRun, actor, requestID, reason string) *model.PrintRunRevision {
	return &model.PrintRunRevision{
		PrintRunID: item.ID, Version: item.Version, Status: item.Status, Name: item.Name,
		Facility: item.Facility, Owner: item.Owner, Category: item.Category,
		RiskLevel: item.RiskLevel, MetricValue: item.MetricValue, MetricUnit: item.MetricUnit,
		Evidence: item.Evidence, RelatedCode: item.RelatedCode,
		ReworkStartedAt: item.ReworkStartedAt, ReworkCount: item.ReworkCount,
		Actor: actor, RequestID: requestID, Reason: reason,
	}
}

// ApplyRework moves a released batch into hold inside one transaction: the
// optimistic-lock update, the immutable revision and the invalidation of
// every 校样 previously linked to the batch either all succeed or nothing is
// persisted, so a conflict never corrupts the version chain.
func (r *printRunRepository) ApplyRework(ctx context.Context, id, version uint, item *model.PrintRun, actor, requestID, reason string) (int64, error) {
	var invalidated int64
	err := r.store.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&model.PrintRun{}).Where("id = ? AND version = ?", id, version).
			Select("*").Omit("id", "code", "created_at", "deleted_at", "Revisions").Updates(item)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return ErrVersionConflict
		}
		if err := tx.Create(printRunRevision(item, actor, requestID, reason)).Error; err != nil {
			return err
		}
		proofs := tx.Model(&model.ColorProof{}).
			Where("related_code = ? AND status <> ?", item.Code, constants.ProofStateInvalid).
			Updates(map[string]any{"status": constants.ProofStateInvalid, "updated_at": item.UpdatedAt})
		if proofs.Error != nil {
			return proofs.Error
		}
		invalidated = proofs.RowsAffected
		return nil
	})
	return invalidated, err
}
func (r *printRunRepository) Delete(ctx context.Context, id uint) error {
	return r.store.Delete(ctx, id)
}
func (r *printRunRepository) CountByStatus(ctx context.Context) (map[string]int64, error) {
	return r.store.CountByStatus(ctx)
}
