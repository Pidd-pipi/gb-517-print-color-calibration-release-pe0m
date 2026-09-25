package repository

import (
	"context"
	"strconv"
	"strings"
	"time"

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
		Preload("Reworks", func(db *gorm.DB) *gorm.DB { return db.Order("id DESC") }).
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
		Actor: actor, RequestID: requestID, Reason: reason,
	}
}
func (r *printRunRepository) Delete(ctx context.Context, id uint) error {
	return r.store.Delete(ctx, id)
}
func (r *printRunRepository) CountByStatus(ctx context.Context) (map[string]int64, error) {
	return r.store.CountByStatus(ctx)
}

// RunReworkRepository owns persistence for 批次返修 records and the
// cross-aggregate transactions that start and close a rework cycle.
type RunReworkRepository interface {
	List(context.Context, dto.ReworkListQuery) (Page[model.RunRework], error)
	Get(context.Context, uint) (model.RunRework, error)
	// OpenForRun returns the waiting rework for a batch plus whether one exists.
	OpenForRun(context.Context, uint) (model.RunRework, bool, error)
	// Start creates the waiting rework, moves the run released -> rework_pending
	// under an optimistic lock, appends an immutable run revision and
	// invalidates every proof previously bound to the batch.
	Start(ctx context.Context, run *model.PrintRun, expectedVersion uint, reason, actor, requestID string, now time.Time) (model.RunRework, error)
	// Complete closes the waiting rework and moves the run back to released
	// under an optimistic lock, recording the replacement proof that passed
	// review and appending the re-release run revision.
	Complete(ctx context.Context, run *model.PrintRun, expectedVersion uint, rework *model.RunRework, proofCode, actor, requestID, reason string, now time.Time) error
}

type runReworkRepository struct {
	db *gorm.DB
}

func NewRunReworkRepository(db *gorm.DB) RunReworkRepository {
	return &runReworkRepository{db: db}
}

func (r *runReworkRepository) List(ctx context.Context, q dto.ReworkListQuery) (Page[model.RunRework], error) {
	page, pageSize := normalizePage(q.Page, q.PageSize)
	db := r.db.WithContext(ctx).Model(&model.RunRework{})
	if status := strings.TrimSpace(q.Status); status != "" {
		db = db.Where("status = ?", status)
	}
	if code := strings.TrimSpace(strings.ToUpper(q.RunCode)); code != "" {
		db = db.Where("run_code = ?", code)
	}
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return Page[model.RunRework]{}, err
	}
	items := make([]model.RunRework, 0)
	err := db.Order("started_at DESC, id DESC").
		Offset((page - 1) * pageSize).Limit(pageSize).
		Preload("InvalidatedProofs").Find(&items).Error
	return Page[model.RunRework]{Items: items, Total: total, Page: page, PageSize: pageSize}, err
}

func (r *runReworkRepository) Get(ctx context.Context, id uint) (model.RunRework, error) {
	var item model.RunRework
	err := r.db.WithContext(ctx).Preload("InvalidatedProofs").First(&item, id).Error
	return item, err
}

func (r *runReworkRepository) OpenForRun(ctx context.Context, runID uint) (model.RunRework, bool, error) {
	var item model.RunRework
	err := r.db.WithContext(ctx).
		Where("print_run_id = ? AND status = ?", runID, "waiting").
		Order("id DESC").First(&item).Error
	if err == gorm.ErrRecordNotFound {
		return model.RunRework{}, false, nil
	}
	if err != nil {
		return model.RunRework{}, false, err
	}
	return item, true, nil
}

// runProofClause binds proofs to a batch. New proofs carry run_code; legacy
// demo data links the batch code through related_code, so both are accepted.
func runProofClause(tx *gorm.DB, runCode string) *gorm.DB {
	code := strings.ToUpper(strings.TrimSpace(runCode))
	return tx.Where("(run_code = ? OR related_code = ?) AND status <> ?", code, code, "invalidated")
}

func (r *runReworkRepository) Start(ctx context.Context, run *model.PrintRun, expectedVersion uint, reason, actor, requestID string, now time.Time) (model.RunRework, error) {
	var created model.RunRework
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// released + version guard makes reason/state/version races conflict.
		result := tx.Model(&model.PrintRun{}).
			Where("id = ? AND version = ? AND status = ?", run.ID, expectedVersion, "released").
			Updates(map[string]any{"status": "rework_pending", "version": expectedVersion + 1, "updated_at": now})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return ErrVersionConflict
		}
		var prior int64
		if err := tx.Model(&model.RunRework{}).Where("print_run_id = ?", run.ID).Count(&prior).Error; err != nil {
			return err
		}
		created = model.RunRework{
			BaseModel: model.BaseModel{
				Code:        run.Code + "-RW" + strconv.FormatInt(prior+1, 10),
				Name:        run.Name + " 批次返修 #" + strconv.FormatInt(prior+1, 10),
				Status:      "waiting",
				Version:     1,
				Description: "放行后色差批次返修", CreatedAt: now, UpdatedAt: now,
			},
			PrintRunID: run.ID, RunCode: run.Code, ReworkCount: uint(prior + 1),
			Reason: reason, StartedAt: now,
		}
		if err := tx.Omit("InvalidatedProofs").Create(&created).Error; err != nil {
			return err
		}
		run.Status = "rework_pending"
		run.Version = expectedVersion + 1
		run.UpdatedAt = now
		if err := tx.Create(printRunRevision(run, actor, requestID, reason)).Error; err != nil {
			return err
		}
		// Every previously usable proof of the batch becomes void and is linked
		// back to this rework for the detail view.
		var proofs []model.ColorProof
		if err := runProofClause(tx.Model(&model.ColorProof{}), run.Code).Find(&proofs).Error; err != nil {
			return err
		}
		if err := tx.Model(&model.ColorProof{}).
			Where("id IN ?", proofIDs(proofs)).
			Updates(map[string]any{"status": "invalidated", "rework_id": created.ID,
				"version": gorm.Expr("version + 1"), "updated_at": now}).Error; err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return model.RunRework{}, err
	}
	return r.Get(ctx, created.ID)
}

func (r *runReworkRepository) Complete(ctx context.Context, run *model.PrintRun, expectedVersion uint, rework *model.RunRework, proofCode, actor, requestID, reason string, now time.Time) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&model.PrintRun{}).
			Where("id = ? AND version = ? AND status = ?", run.ID, expectedVersion, "rework_pending").
			Updates(map[string]any{"status": "released", "version": expectedVersion + 1, "updated_at": now})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return ErrVersionConflict
		}
		closed := tx.Model(&model.RunRework{}).
			Where("id = ? AND status = ?", rework.ID, "waiting").
			Updates(map[string]any{
				"status": "released", "completed_at": now,
				"resolution_proof_code": proofCode,
				"version":               gorm.Expr("version + 1"), "updated_at": now,
			})
		if closed.Error != nil {
			return closed.Error
		}
		if closed.RowsAffected == 0 {
			return ErrVersionConflict
		}
		run.Status = "released"
		run.Version = expectedVersion + 1
		run.UpdatedAt = now
		return tx.Create(printRunRevision(run, actor, requestID, reason)).Error
	})
}

func proofIDs(proofs []model.ColorProof) []uint {
	ids := make([]uint, 0, len(proofs))
	for _, proof := range proofs {
		ids = append(ids, proof.ID)
	}
	return ids
}
