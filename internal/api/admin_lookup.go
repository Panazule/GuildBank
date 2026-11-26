package api

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/Panazule/guildbank/internal/domain"
	"github.com/Panazule/guildbank/internal/service"
)

// itemCandidateResponse is one candidate in a name lookup or an ambiguity error.
type itemCandidateResponse struct {
	EQUID uint64 `json:"equid"`
	Name  string `json:"name"`
}

func itemCandidatesOf(items []domain.Item) []itemCandidateResponse {
	out := make([]itemCandidateResponse, 0, len(items))
	for _, item := range items {
		out = append(out, itemCandidateResponse{EQUID: item.EQUID, Name: item.Name})
	}
	return out
}

func derefString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

// itemNames resolves an entered item name against the read-only Items source.
// It backs the name-first create form; the create path runs the same resolution
// authoritatively, so the hint and the stored value cannot disagree.
//
// Outcomes: uniquely matched (item), ambiguous (candidates, never guessed) and
// not found (the create path then stores the unresolved-identity placeholder).
func (h *adminInventoryHandler) itemNames(c *gin.Context) {
	name := strings.TrimSpace(c.Query("name"))
	if name == "" {
		writeErrorFields(c, http.StatusUnprocessableEntity, "validation_failed",
			"one or more fields are invalid", map[string]string{"name": "name is required"})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	resolution, err := h.admin.ResolveItemName(ctx, name)
	if err != nil {
		h.respondAdminError(c, err)
		return
	}
	body := gin.H{"name": resolution.Name, "status": string(resolution.Status)}
	switch resolution.Status {
	case service.ItemNameStatusUnique:
		body["item"] = itemCandidateResponse{EQUID: resolution.Match.EQUID, Name: resolution.Match.Name}
		body["caseInsensitive"] = resolution.CaseInsensitive
	case service.ItemNameStatusAmbiguous:
		body["candidates"] = itemCandidatesOf(resolution.Candidates)
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"data": body})
}
