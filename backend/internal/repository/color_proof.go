package repository

import (
	"context"
	"strings"
	"time"

	"github.com/blueship581/print-color-calibration-release/backend/internal/dto"
	"github.com/blueship581/print-color-calibration-release/backend/internal/model"
	"gorm.io/gorm"
)

// ColorProofRepository owns all persistence operations for 色彩校样.
type ColorProofRepository interface {
	List(context.Context, dto.PageQuery, string) (Page[model.ColorProof], error)
	Get(context.Context, uint) (model.ColorProof, error)
	Create(context.Context, *model.ColorProof) error
	Update(context.Context, uint, uint, *model.ColorProof) error
	Delete(context.Context, uint) error
	CountByStatus(context.Context) (map[string]int64, error)
	// LatestAcceptedForRun returns the most recent accepted proof bound to the
	// batch that was created after the given time. Only such a replacement
	// proof satisfies the re-release gate.
	LatestAcceptedForRun(context.Context, string, time.Time) (model.ColorProof, bool, error)
	// GetByCode loads a proof regardless of its current status, used to show
	// the recorded resolution proof of a completed rework.
	GetByCode(context.Context, string) (model.ColorProof, bool, error)
}

type colorProofRepository struct {
	store *Store[model.ColorProof]
}

func NewColorProofRepository(db *gorm.DB) ColorProofRepository {
	return &colorProofRepository{store: NewStore[model.ColorProof](db)}
}

func (r *colorProofRepository) List(ctx context.Context, q dto.PageQuery, runCode string) (Page[model.ColorProof], error) {
	page, pageSize := normalizePage(q.Page, q.PageSize)
	db := r.store.db.WithContext(ctx).Model(&model.ColorProof{})
	if search := strings.TrimSpace(strings.ToLower(q.Search)); search != "" {
		wildcard := "%" + search + "%"
		db = db.Where("LOWER(code) LIKE ? OR LOWER(name) LIKE ?", wildcard, wildcard)
	}
	if status := strings.TrimSpace(q.Status); status != "" {
		db = db.Where("status = ?", status)
	}
	if code := strings.TrimSpace(strings.ToUpper(runCode)); code != "" {
		db = db.Where("run_code = ?", code)
	}
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return Page[model.ColorProof]{}, err
	}
	items := make([]model.ColorProof, 0)
	err := db.Order("updated_at DESC, id DESC").
		Offset((page - 1) * pageSize).Limit(pageSize).Find(&items).Error
	return Page[model.ColorProof]{Items: items, Total: total, Page: page, PageSize: pageSize}, err
}
func (r *colorProofRepository) Get(ctx context.Context, id uint) (model.ColorProof, error) {
	return r.store.Get(ctx, id)
}
func (r *colorProofRepository) Create(ctx context.Context, item *model.ColorProof) error {
	return r.store.Create(ctx, item)
}
func (r *colorProofRepository) Update(ctx context.Context, id, version uint, item *model.ColorProof) error {
	return r.store.Update(ctx, id, version, item)
}
func (r *colorProofRepository) Delete(ctx context.Context, id uint) error {
	return r.store.Delete(ctx, id)
}
func (r *colorProofRepository) CountByStatus(ctx context.Context) (map[string]int64, error) {
	return r.store.CountByStatus(ctx)
}

func (r *colorProofRepository) LatestAcceptedForRun(ctx context.Context, runCode string, since time.Time) (model.ColorProof, bool, error) {
	var item model.ColorProof
	err := r.store.db.WithContext(ctx).
		Where("(run_code = ? OR related_code = ?) AND status = ? AND created_at >= ?",
			strings.ToUpper(strings.TrimSpace(runCode)), strings.ToUpper(strings.TrimSpace(runCode)),
			"accepted", since).
		Order("created_at DESC, id DESC").First(&item).Error
	if err == gorm.ErrRecordNotFound {
		return model.ColorProof{}, false, nil
	}
	if err != nil {
		return model.ColorProof{}, false, err
	}
	return item, true, nil
}

func (r *colorProofRepository) GetByCode(ctx context.Context, code string) (model.ColorProof, bool, error) {
	var item model.ColorProof
	err := r.store.db.WithContext(ctx).
		Where("code = ?", strings.ToUpper(strings.TrimSpace(code))).First(&item).Error
	if err == gorm.ErrRecordNotFound {
		return model.ColorProof{}, false, nil
	}
	if err != nil {
		return model.ColorProof{}, false, err
	}
	return item, true, nil
}
