package handler

import (
	"net/http"

	"github.com/blueship581/print-color-calibration-release/backend/internal/dto"
	"github.com/blueship581/print-color-calibration-release/backend/internal/middleware"
	"github.com/blueship581/print-color-calibration-release/backend/internal/service"
	"github.com/blueship581/print-color-calibration-release/backend/internal/util"
	"github.com/gin-gonic/gin"
)

type PrintRunHandler struct{ service service.PrintRunService }

func NewPrintRunHandler(s service.PrintRunService) *PrintRunHandler {
	return &PrintRunHandler{service: s}
}

func (h *PrintRunHandler) Register(group *gin.RouterGroup) {
	resource := group.Group("/runs")
	resource.GET("", h.list)
	resource.GET("/:id", h.get)
	resource.POST("", middleware.RequireMinimumRole("operator"), h.create)
	resource.PUT("/:id", middleware.RequireMinimumRole("operator"), h.update)
	resource.POST("/:id/transition", middleware.RequireMinimumRole("operator"), h.transition)
	resource.DELETE("/:id", middleware.RequireRoles("admin"), h.remove)
}

func (h *PrintRunHandler) list(c *gin.Context) {
	query := bindPage(c)
	result, err := h.service.List(c.Request.Context(), query)
	if err != nil {
		handleError(c, err)
		return
	}
	util.Page(c, result.Items, result.Page, result.PageSize, result.Total)
}

func (h *PrintRunHandler) get(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	item, err := h.service.Get(c.Request.Context(), id)
	if err != nil {
		handleError(c, err)
		return
	}
	util.OK(c, item)
}

func (h *PrintRunHandler) create(c *gin.Context) {
	var input dto.CreatePrintRun
	if err := c.ShouldBindJSON(&input); err != nil {
		util.Fail(c, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	item, err := h.service.Create(c.Request.Context(), input, actorFromContext(c), requestIDFromContext(c))
	if err != nil {
		handleError(c, err)
		return
	}
	util.Created(c, item)
}

func (h *PrintRunHandler) update(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	var input dto.UpdatePrintRun
	if err := c.ShouldBindJSON(&input); err != nil {
		util.Fail(c, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	item, err := h.service.Update(c.Request.Context(), id, input, actorFromContext(c), requestIDFromContext(c))
	if err != nil {
		handleError(c, err)
		return
	}
	util.OK(c, item)
}

func (h *PrintRunHandler) transition(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	var input dto.TransitionRequest
	if err := c.ShouldBindJSON(&input); err != nil {
		util.Fail(c, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	item, err := h.service.Transition(c.Request.Context(), id, input, actorFromContext(c), roleFromContext(c), requestIDFromContext(c))
	if err != nil {
		handleError(c, err)
		return
	}
	util.OK(c, item)
}

func (h *PrintRunHandler) remove(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	if err := h.service.Delete(c.Request.Context(), id, actorFromContext(c), requestIDFromContext(c)); err != nil {
		handleError(c, err)
		return
	}
	util.NoContent(c)
}

// RunReworkHandler exposes 批次返修: reviewer-only start on a released batch,
// plus read endpoints for the waiting history and detail (start time,
// invalidated proofs, re-release condition).
type RunReworkHandler struct{ service service.RunReworkService }

func NewRunReworkHandler(s service.RunReworkService) *RunReworkHandler {
	return &RunReworkHandler{service: s}
}

func (h *RunReworkHandler) Register(group *gin.RouterGroup) {
	// Start endpoint lives under the batch aggregate.
	group.POST("/runs/:id/rework", middleware.RequireMinimumRole("reviewer"), h.startForRun)

	resource := group.Group("/reworks")
	resource.GET("", h.list)
	resource.GET("/:id", h.get)
}

func (h *RunReworkHandler) startForRun(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	var input dto.StartReworkRequest
	if err := c.ShouldBindJSON(&input); err != nil {
		util.Fail(c, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	rework, err := h.service.Start(c.Request.Context(), id, input,
		actorFromContext(c), roleFromContext(c), requestIDFromContext(c))
	if err != nil {
		handleError(c, err)
		return
	}
	util.Created(c, rework)
}

func (h *RunReworkHandler) list(c *gin.Context) {
	var query dto.ReworkListQuery
	_ = c.ShouldBindQuery(&query)
	if query.Page < 1 {
		query.Page = 1
	}
	if query.PageSize < 1 || query.PageSize > 100 {
		query.PageSize = 20
	}
	result, err := h.service.List(c.Request.Context(), query)
	if err != nil {
		handleError(c, err)
		return
	}
	util.Page(c, result.Items, result.Page, result.PageSize, result.Total)
}

func (h *RunReworkHandler) get(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	detail, err := h.service.Get(c.Request.Context(), id)
	if err != nil {
		handleError(c, err)
		return
	}
	util.OK(c, detail)
}
