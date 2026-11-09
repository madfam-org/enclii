package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strings"
	"unicode"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/k8s"
	"github.com/madfam-org/enclii/apps/switchyard-api/internal/stagingintents"
)

type januaStagingIntentRequest struct {
	ID       string `json:"operation_id"`
	Record   *bool  `json:"record"`
	Expected string `json:"expect_plan_sha256,omitempty"`
	Reason   string `json:"reason,omitempty"`
}

var januaStagingIntentHash = regexp.MustCompile(`^[a-f0-9]{64}$`)

func canonicalStagingIntentID(value string) (uuid.UUID, bool) {
	id, err := uuid.Parse(value)
	return id, err == nil && id != uuid.Nil && id.String() == value
}
func parseJanuaStagingIntent(raw []byte) (januaStagingIntentRequest, bool) {
	var r januaStagingIntentRequest
	var keys map[string]json.RawMessage
	if k8s.DecodeInitialBootstrapJSON(raw, &keys) != nil {
		return r, false
	}
	for key := range keys {
		if key != "operation_id" && key != "record" && key != "expect_plan_sha256" && key != "reason" {
			return r, false
		}
	}
	if k8s.DecodeInitialBootstrapJSON(raw, &r) != nil || r.Record == nil {
		return r, false
	}
	if _, ok := canonicalStagingIntentID(r.ID); !ok {
		return r, false
	}
	if !*r.Record {
		return r, len(keys) == 2 && r.Expected == "" && r.Reason == ""
	}
	return r, len(keys) == 4 && januaStagingIntentHash.MatchString(r.Expected) && r.Reason != "" && strings.TrimSpace(r.Reason) == r.Reason && len(r.Reason) <= 512 && strings.IndexFunc(r.Reason, unicode.IsControl) < 0
}
func registerJanuaStagingIntentRoutes(platform *gin.RouterGroup, h *Handler) {
	platform.POST("/projects/:slug/environments/staging/preregistration-intents", h.RequirePlatformOperator(), h.JanuaStagingIntent)
	platform.GET("/projects/:slug/environments/staging/preregistration-intents/:operation_id", h.RequirePlatformOperator(), h.ReadJanuaStagingIntent)
}
func (h *Handler) januaStagingIntentStore(c *gin.Context) (stagingintents.Store, bool) {
	if c.Param("slug") != "janua" || c.Request.URL.RawQuery != "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "fixed Janua staging intent route required"})
		return stagingintents.Store{}, false
	}
	if h.repos == nil || h.repos.DB() == nil || h.repos.Projects == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "held intent adapter unavailable"})
		return stagingintents.Store{}, false
	}
	p, err := h.repos.Projects.GetBySlug("janua")
	if err != nil || p == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "project unavailable"})
		return stagingintents.Store{}, false
	}
	if !h.enforceUserProjectAccess(c, p.ID) {
		return stagingintents.Store{}, false
	}
	return stagingintents.Store{DB: h.repos.DB(), ProjectID: p.ID}, true
}

// This records only an inert immutable intent; there is no service/native adapter.
func (h *Handler) JanuaStagingIntent(c *gin.Context) {
	b, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, 8192))
	r, ok := parseJanuaStagingIntent(b)
	actor, actorOK := canonicalStagingIntentID(c.GetString("user_id"))
	if err != nil || !ok || !actorOK {
		c.JSON(http.StatusBadRequest, gin.H{"error": "closed held-intent input required"})
		return
	}
	store, ok := h.januaStagingIntentStore(c)
	if !ok {
		return
	}
	id, _ := uuid.Parse(r.ID)
	if !*r.Record {
		receipt, err := store.Plan(c.Request.Context(), actor, id)
		if err != nil {
			c.JSON(http.StatusConflict, gin.H{"error": "held intent ownership unavailable"})
			return
		}
		c.JSON(http.StatusOK, receipt)
		return
	}
	receipt, err := store.Record(c.Request.Context(), actor, id, r.Expected, r.Reason)
	if err != nil {
		status := http.StatusConflict
		message := "held intent refused; no activation was requested"
		if errors.Is(err, stagingintents.ErrCommitUnknown) {
			status = http.StatusServiceUnavailable
			message = stagingintents.ErrCommitUnknown.Error()
		}
		c.JSON(status, gin.H{"error": message})
		return
	}
	c.JSON(http.StatusOK, receipt)
}
func (h *Handler) ReadJanuaStagingIntent(c *gin.Context) {
	id, ok := canonicalStagingIntentID(c.Param("operation_id"))
	if !ok || c.Request.ContentLength != 0 || len(c.Request.TransferEncoding) != 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "exact held-intent UUID required"})
		return
	}
	store, ok := h.januaStagingIntentStore(c)
	if !ok {
		return
	}
	r, err := store.Read(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "held intent unavailable"})
		return
	}
	c.JSON(http.StatusOK, r)
}
