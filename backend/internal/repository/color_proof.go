package repository

import (
	"context"
	"time"

	"github.com/blueship581/print-color-calibration-release/backend/internal/dto"
	"github.com/blueship581/print-color-calibration-release/backend/internal/model"
	"gorm.io/gorm"
)

// ColorProofRepository owns all persistence operations for 色彩校样.
type ColorProofRepository interface {
	List(context.Context, dto.PageQuery) (Page[model.ColorProof], error)
	Get(context.Context, uint) (model.ColorProof, error)
	Create(context.Context, *model.ColorProof) error
	Update(context.Context, uint, uint, *model.ColorProof) error
	Delete(context.Context, uint) error
	CountByStatus(context.Context) (map[string]int64, error)
	ListByRelatedCode(context.Context, string) ([]model.ColorProof, error)
	CountAcceptedRelatedSince(context.Context, string, time.Time) (int64, error)
}

type colorProofRepository struct {
	store *Store[model.ColorProof]
}

func NewColorProofRepository(db *gorm.DB) ColorProofRepository {
	return &colorProofRepository{store: NewStore[model.ColorProof](db)}
}

func (r *colorProofRepository) List(ctx context.Context, q dto.PageQuery) (Page[model.ColorProof], error) {
	return r.store.List(ctx, q)
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

// ListByRelatedCode returns every 校样 linked to a batch code, newest first,
// so the rework detail can show exactly which proofs were invalidated.
func (r *colorProofRepository) ListByRelatedCode(ctx context.Context, relatedCode string) ([]model.ColorProof, error) {
	items := make([]model.ColorProof, 0)
	err := r.store.db.WithContext(ctx).
		Where("related_code = ?", relatedCode).
		Order("updated_at DESC, id DESC").
		Find(&items).Error
	return items, err
}

// CountAcceptedRelatedSince counts reviewer-accepted 校样 captured for the
// batch after a rework started; it is the gate for releasing the batch again.
func (r *colorProofRepository) CountAcceptedRelatedSince(ctx context.Context, relatedCode string, since time.Time) (int64, error) {
	var total int64
	err := r.store.db.WithContext(ctx).Model(&model.ColorProof{}).
		Where("related_code = ? AND status = ? AND created_at >= ?", relatedCode, "accepted", since).
		Count(&total).Error
	return total, err
}
