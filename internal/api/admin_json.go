package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/Panazule/guildbank/internal/domain"
	"github.com/Panazule/guildbank/internal/service"
)

// AdminMaxBodySize is the handler-level cap for administrative JSON and form
// writes. It narrows the global 1 MiB middleware cap; a valid write is a few
// hundred bytes.
const AdminMaxBodySize = 64 << 10

// requestError is a decode/parse failure with its documented status mapping.
type requestError struct {
	status  int
	code    string
	message string
	fields  map[string]string
}

func (e *requestError) Error() string { return e.message }

func badRequest(message string) *requestError {
	return &requestError{status: http.StatusBadRequest, code: "invalid_request", message: message}
}

func validationRequestError(fields map[string]string) *requestError {
	return &requestError{
		status: http.StatusUnprocessableEntity, code: "validation_failed",
		message: "one or more fields are invalid", fields: fields,
	}
}

// decimalUint accepts a JSON number or a decimal string, rejecting signs,
// fractions, exponents, whitespace and non-digits.
type decimalUint struct {
	Value uint64
	Set   bool
}

var errDecimalFormat = errors.New("value must be a non-negative decimal integer")

func (d *decimalUint) UnmarshalJSON(data []byte) error {
	raw := strings.TrimSpace(string(data))
	if raw == "null" {
		return nil
	}
	if strings.HasPrefix(raw, `"`) {
		var text string
		if err := json.Unmarshal(data, &text); err != nil {
			return errDecimalFormat
		}
		raw = text
	}
	if raw == "" {
		return errDecimalFormat
	}
	for _, r := range raw {
		if r < '0' || r > '9' {
			return errDecimalFormat
		}
	}
	value, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		return errDecimalFormat
	}
	d.Value, d.Set = value, true
	return nil
}

// decodeAdminJSON enforces the documented strict contract: JSON content type, a
// 64 KiB body cap, no duplicate object keys, no unknown fields and no trailing
// value.
func decodeAdminJSON(c *gin.Context, dst any) *requestError {
	contentType := strings.ToLower(strings.TrimSpace(c.GetHeader("Content-Type")))
	if !strings.HasPrefix(contentType, "application/json") {
		return badRequest("Content-Type must be application/json")
	}
	body := http.MaxBytesReader(c.Writer, c.Request.Body, AdminMaxBodySize)
	data, err := io.ReadAll(body)
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			return &requestError{status: http.StatusRequestEntityTooLarge, code: "request_too_large",
				message: "request body must be at most 64 KiB"}
		}
		return badRequest("request body could not be read")
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return badRequest("request body is required")
	}
	if err := rejectDuplicateJSONKeys(data); err != nil {
		return badRequest("request body contains a duplicate or malformed field")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		return badRequest("request body is not valid for this endpoint: " + err.Error())
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return badRequest("request body must contain exactly one JSON value")
	}
	return nil
}

// rejectDuplicateJSONKeys walks the token stream and rejects a repeated key in
// the same object, at any depth. A repeated key would otherwise silently
// overwrite an earlier value before struct decoding.
func rejectDuplicateJSONKeys(data []byte) error {
	type frame struct {
		object    bool
		expectKey bool
		keys      map[string]bool
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	stack := make([]frame, 0, 8)
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			if len(stack) != 0 {
				return fmt.Errorf("truncated JSON object or array")
			}
			return nil
		}
		if err != nil {
			return err
		}
		switch value := token.(type) {
		case json.Delim:
			switch value {
			case '{':
				if len(stack) > 0 {
					stack[len(stack)-1].expectKey = true
				}
				stack = append(stack, frame{object: true, expectKey: true, keys: map[string]bool{}})
			case '[':
				if len(stack) > 0 {
					stack[len(stack)-1].expectKey = true
				}
				stack = append(stack, frame{})
			case '}', ']':
				if len(stack) == 0 {
					return fmt.Errorf("unbalanced JSON")
				}
				stack = stack[:len(stack)-1]
				if len(stack) > 0 {
					stack[len(stack)-1].expectKey = true
				}
			}
		case string:
			if len(stack) == 0 {
				continue
			}
			top := &stack[len(stack)-1]
			if top.object && top.expectKey {
				if top.keys[value] {
					return fmt.Errorf("duplicate key %q", value)
				}
				top.keys[value] = true
				top.expectKey = false
				continue
			}
			top.expectKey = true
		default:
			if len(stack) > 0 {
				stack[len(stack)-1].expectKey = true
			}
		}
	}
}

// createItemRequest is the create DTO. Optional strings and dates are raw so
// "absent", "null" and a value stay distinguishable.
type createItemRequest struct {
	ItemType       *string          `json:"itemType"`
	EQUID          decimalUint      `json:"equid"`
	Quantity       decimalUint      `json:"quantity"`
	SourceLocation *json.RawMessage `json:"sourceLocation"`
	GivenTo        *json.RawMessage `json:"givenTo"`
	Name           *string          `json:"name"`
	DateOpen       *json.RawMessage `json:"dateOpen"`
	DateBazaar     *json.RawMessage `json:"dateBazaar"`

	// Server-derived or immutable fields are listed explicitly so a client that
	// sends one receives 422 naming it instead of a generic unknown-field 400.
	ID              *json.RawMessage `json:"id"`
	DateAdded       *json.RawMessage `json:"dateAdded"`
	ClassString     *json.RawMessage `json:"classString"`
	RaceString      *json.RawMessage `json:"raceString"`
	MetadataMissing *json.RawMessage `json:"metadataMissing"`
	LocationGroup   *json.RawMessage `json:"locationGroup"`
	LocationSort    *json.RawMessage `json:"locationSort"`
	NameSort        *json.RawMessage `json:"nameSort"`
	Deleted         *json.RawMessage `json:"deleted"`
	ModAt           *json.RawMessage `json:"modAt"`
	ModBy           *json.RawMessage `json:"modBy"`
	ModByUserID     *json.RawMessage `json:"modByUserId"`
	Availability    *json.RawMessage `json:"availability"`
}

// updateItemRequest is the update DTO. Quantity is deliberately absent: the
// generic update contract cannot change stock levels. Quantity changes are
// recorded by the issue, return and future adjust operations only.
type updateItemRequest struct {
	ExpectedModAt  *string          `json:"expectedModAt"`
	SourceLocation *json.RawMessage `json:"sourceLocation"`
	// Quantity is decoded only so a client that sends it receives a named 422
	// instead of a generic unknown-field 400.
	Quantity   *json.RawMessage `json:"quantity"`
	GivenTo    *json.RawMessage `json:"givenTo"`
	DateOpen   *json.RawMessage `json:"dateOpen"`
	DateBazaar *json.RawMessage `json:"dateBazaar"`
	// Remove asks for the same transaction to soft-delete the record (AI-02).
	Remove *bool `json:"remove"`

	ID              *json.RawMessage `json:"id"`
	EQUID           *json.RawMessage `json:"equid"`
	Name            *json.RawMessage `json:"name"`
	ItemType        *json.RawMessage `json:"itemType"`
	DateAdded       *json.RawMessage `json:"dateAdded"`
	ClassString     *json.RawMessage `json:"classString"`
	RaceString      *json.RawMessage `json:"raceString"`
	MetadataMissing *json.RawMessage `json:"metadataMissing"`
	LocationGroup   *json.RawMessage `json:"locationGroup"`
	LocationSort    *json.RawMessage `json:"locationSort"`
	NameSort        *json.RawMessage `json:"nameSort"`
	ModBy           *json.RawMessage `json:"modBy"`
	ModByUserID     *json.RawMessage `json:"modByUserId"`
}

type adminItemResponse struct {
	ID              int64                    `json:"id"`
	EQUID           uint64                   `json:"equid"`
	Name            string                   `json:"name"`
	ItemType        domain.ItemType          `json:"itemType"`
	Quantity        uint32                   `json:"quantity"`
	ClassString     string                   `json:"classString"`
	RaceString      string                   `json:"raceString"`
	MetadataMissing bool                     `json:"metadataMissing"`
	SourceLocation  *string                  `json:"sourceLocation"`
	LocationGroup   string                   `json:"locationGroup"`
	LocationSort    string                   `json:"locationSort"`
	GivenTo         *string                  `json:"givenTo"`
	Deleted         bool                     `json:"deleted"`
	NameSort        string                   `json:"nameSort"`
	DateAdded       string                   `json:"dateAdded"`
	DateOpen        string                   `json:"dateOpen"`
	DateBazaar      string                   `json:"dateBazaar"`
	Availability    domain.AvailabilityState `json:"availability"`
	ModAt           string                   `json:"modAt"`
	ModBy           string                   `json:"modBy"`
	ModByUserID     *int64                   `json:"modByUserId"`
}

type adminWarningResponse struct {
	Code   string `json:"code"`
	Field  string `json:"field,omitempty"`
	Detail string `json:"detail,omitempty"`
}

// immutableFields rejects any server-derived or immutable key present in a
// create or update body, naming the first offending field.
func immutableFields(pairs []struct {
	name string
	raw  *json.RawMessage
}) map[string]string {
	for _, pair := range pairs {
		if pair.raw != nil {
			return map[string]string{pair.name: pair.name + " is not editable"}
		}
	}
	return nil
}

// parseOptionalString maps an absent key, null and a string value.
func parseOptionalString(field string, raw *json.RawMessage, fields map[string]string) domain.OptionalString {
	if raw == nil {
		return domain.OptionalString{}
	}
	trimmed := strings.TrimSpace(string(*raw))
	if trimmed == "null" {
		return domain.OptionalString{Set: true}
	}
	var value string
	if err := json.Unmarshal(*raw, &value); err != nil {
		fields[field] = field + " must be a string or null"
		return domain.OptionalString{Set: true}
	}
	return domain.OptionalString{Value: &value, Set: true}
}

// parseOptionalDate maps an absent key, null and a strict YYYY-MM-DD string.
func parseOptionalDate(field string, raw *json.RawMessage, fields map[string]string) domain.OptionalDate {
	if raw == nil {
		return domain.OptionalDate{}
	}
	trimmed := strings.TrimSpace(string(*raw))
	if trimmed == "null" {
		return domain.OptionalDate{Set: true}
	}
	var text string
	if err := json.Unmarshal(*raw, &text); err != nil {
		fields[field] = field + " must be YYYY-MM-DD or null"
		return domain.OptionalDate{Set: true}
	}
	value, err := time.Parse("2006-01-02", text)
	if err != nil {
		fields[field] = field + " must be YYYY-MM-DD or null"
		return domain.OptionalDate{Set: true}
	}
	return domain.OptionalDate{Value: &value, Set: true}
}

// parseExpectedModAt parses RFC 3339 with an optional fraction and normalizes
// it to microseconds.
func parseExpectedModAt(raw string) (time.Time, bool) {
	value, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(raw))
	if err != nil {
		return time.Time{}, false
	}
	return value.UTC().Truncate(time.Microsecond), true
}

// writeRequestError maps a requestError onto the shared error envelope.
func writeRequestError(c *gin.Context, err *requestError) {
	writeErrorFields(c, err.status, err.code, err.message, err.fields)
}

// adminActor builds the actor snapshot from the authenticated session.
func adminActor(c *gin.Context) (domain.Actor, bool) {
	session, ok := CurrentSession(c)
	if !ok {
		writeError(c, http.StatusUnauthorized, "unauthenticated", "authentication required")
		return domain.Actor{}, false
	}
	return domain.Actor{
		UserID: session.User.ID, UserName: session.User.UserName, IsAdmin: session.User.IsAdmin(),
	}, true
}

// respondAdminError maps admin service errors, adding the settings-fault 503
// that PD5-27 requires for an inverted derived window.
func (h *adminInventoryHandler) respondAdminError(c *gin.Context, err error) {
	var validation *domain.ValidationError
	switch {
	case errors.As(err, &validation):
		writeErrorFields(c, http.StatusUnprocessableEntity, "validation_failed",
			"one or more fields are invalid", validation.Fields)
	case errors.Is(err, service.ErrSettingsInvalid):
		writeError(c, http.StatusServiceUnavailable, "dependency_unavailable",
			"the guild date settings are invalid; an operator must correct them")
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled),
		errors.Is(err, domain.ErrUnavailable):
		writeError(c, http.StatusServiceUnavailable, "dependency_unavailable",
			"the guild inventory is unavailable")
	default:
		respondError(c, h.logger, err)
	}
}
