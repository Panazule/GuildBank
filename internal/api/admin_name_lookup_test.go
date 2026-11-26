package api

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/Panazule/guildbank/internal/domain"
)

func discardAdminLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// A named-but-omitted EQUID must reach the service resolution instead of being
// rejected by the request decoder.
func TestCreateInputFromAcceptsNameWithoutEQUID(t *testing.T) {
	itemType := "Item"
	name := "Sword"
	req := createItemRequest{
		ItemType: &itemType, Name: &name,
		Quantity: decimalUint{Value: 2, Set: true},
	}
	in, reqErr := createInputFrom(req)
	if reqErr != nil {
		t.Fatalf("createInputFrom: %v", reqErr)
	}
	if in.EQUID != 0 {
		t.Fatalf("EQUID = %d, want 0 (unresolved by the service)", in.EQUID)
	}
	if in.Name != "Sword" {
		t.Fatalf("Name = %q", in.Name)
	}
}

func TestCreateInputFromRejectsReservedEQUID(t *testing.T) {
	itemType := "Item"
	name := "Sword"
	req := createItemRequest{
		ItemType: &itemType, Name: &name,
		EQUID:    decimalUint{Value: domain.UnresolvedEQUID, Set: true},
		Quantity: decimalUint{Value: 1, Set: true},
	}
	_, reqErr := createInputFrom(req)
	if reqErr == nil {
		t.Fatal("reserved EQUID accepted")
	}
	if _, ok := reqErr.fields["equid"]; !ok {
		t.Fatalf("fields = %+v, want an equid error", reqErr.fields)
	}
}

func TestCreateInputFromRequiresNameWhenEQUIDAbsent(t *testing.T) {
	itemType := "Item"
	req := createItemRequest{
		ItemType: &itemType,
		Quantity: decimalUint{Value: 1, Set: true},
	}
	_, reqErr := createInputFrom(req)
	if reqErr == nil {
		t.Fatal("a create with neither an EQUID nor a name was accepted")
	}
	if _, ok := reqErr.fields["equid"]; !ok {
		t.Fatalf("fields = %+v, want an equid error", reqErr.fields)
	}
}

// The ambiguity response must carry the candidate list so a JSON client can
// offer a choice instead of guessing.
func TestRespondAdminErrorCarriesNameCandidates(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/items", nil)

	h := &adminInventoryHandler{logger: discardAdminLogger()}
	h.respondAdminError(c, &domain.ItemNameAmbiguousError{
		Name: "Ring",
		Candidates: []domain.Item{
			{EQUID: 10, Name: "Ring"},
			{EQUID: 11, Name: "Ring of Fire"},
		},
	})

	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", recorder.Code)
	}
	var body struct {
		Error struct {
			Code       string            `json:"code"`
			Fields     map[string]string `json:"fields"`
			Candidates []struct {
				EQUID uint64 `json:"equid"`
				Name  string `json:"name"`
			} `json:"candidates"`
		} `json:"error"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Error.Code != "validation_failed" {
		t.Fatalf("code = %q", body.Error.Code)
	}
	if _, ok := body.Error.Fields["equid"]; !ok {
		t.Fatalf("fields = %+v, want an equid error", body.Error.Fields)
	}
	if len(body.Error.Candidates) != 2 || body.Error.Candidates[1].EQUID != 11 {
		t.Fatalf("candidates = %+v, want both matches", body.Error.Candidates)
	}
}

// The JSON row projection must tell a client that the identity is unresolved.
func TestAdminItemResponseMarksUnresolvedIdentity(t *testing.T) {
	placeholder := adminItemResponseOf(domain.AdminInventoryItem{EQUID: domain.UnresolvedEQUID, Name: "Mystery"})
	if !placeholder.UnresolvedIdentity {
		t.Fatal("placeholder row not marked unresolved")
	}
	resolved := adminItemResponseOf(domain.AdminInventoryItem{EQUID: 100, Name: "Sword"})
	if resolved.UnresolvedIdentity {
		t.Fatal("resolved row marked unresolved")
	}
}
