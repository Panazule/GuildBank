package api

import (
	"embed"
	"fmt"
	"html/template"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/Panazule/guildbank/internal/domain"
	"github.com/Panazule/guildbank/internal/service"
)

// views renders the embedded server-side templates. No template is loaded
// from disk at runtime, so a static binary is self-contained.
type views struct {
	templates *template.Template
}

func newViews(fs embed.FS) (*views, error) {
	t, err := template.ParseFS(fs, "templates/*.html")
	if err != nil {
		return nil, fmt.Errorf("api: parse templates: %w", err)
	}
	return &views{templates: t}, nil
}

func (v *views) render(c *gin.Context, status int, name string, data any) {
	c.Status(status)
	c.Header("Content-Type", "text/html; charset=utf-8")
	if err := v.templates.ExecuteTemplate(c.Writer, name, data); err != nil {
		// The header is already written; log through Gin's error channel.
		_ = c.Error(err)
	}
}

type pageData struct {
	Title       string
	CSRF        string
	User        *domain.User
	Error       string
	Notice      string
	ErrorFields map[string]string
}

type loginPageData struct {
	pageData
	Next string
}

type setupPageData struct {
	pageData
	Form service.SetupInput
}

type settingView struct {
	Key        string
	Label      string
	Value      string
	Type       domain.SettingType
	Secret     bool
	Configured bool
	Help       string
}

type settingsPageData struct {
	pageData
	Settings []settingView
	Form     map[string]string
}

type homePageData struct {
	pageData
}

type errorPageData struct {
	pageData
	Status int
}

type importBatchView struct {
	ID               int64
	FileName         string
	SHA256           string
	SHA256Prefix     string
	Status           domain.ImportStatus
	SourceRowCount   int
	AcceptedRowCount int
	FailureReason    string
	ImportedBy       string
	ImportedAt       string
	ReadOnly         bool
}

type importsPageData struct {
	pageData
	Ready   bool
	Batches []importBatchView
}

type importDetailPageData struct {
	pageData
	Ready        bool
	Batch        importBatchView
	Uploaded     bool
	FailedUpload bool
}

func (v *views) login(c *gin.Context, status int, data loginPageData) {
	v.render(c, status, "login.html", data)
}

func (v *views) setup(c *gin.Context, status int, data setupPageData) {
	v.render(c, status, "setup.html", data)
}

func (v *views) settings(c *gin.Context, status int, data settingsPageData) {
	v.render(c, status, "settings.html", data)
}

func (v *views) home(c *gin.Context, data homePageData) {
	v.render(c, http.StatusOK, "home.html", data)
}

func (v *views) errorPage(c *gin.Context, status int, message string) {
	v.render(c, status, "error.html", errorPageData{
		pageData: pageData{
			Title: http.StatusText(status),
			CSRF:  CSRF(c),
			Error: message,
		},
		Status: status,
	})
}

func (v *views) imports(c *gin.Context, status int, data importsPageData) {
	v.render(c, status, "imports.html", data)
}

func (v *views) importDetail(c *gin.Context, status int, data importDetailPageData) {
	v.render(c, status, "import_detail.html", data)
}

func importBatchViews(batches []domain.ImportBatch) []importBatchView {
	out := make([]importBatchView, 0, len(batches))
	for _, batch := range batches {
		out = append(out, importBatchViewOf(batch))
	}
	return out
}

func importBatchViewOf(batch domain.ImportBatch) importBatchView {
	sha := ""
	if batch.FileSha256 != nil {
		sha = *batch.FileSha256
	}
	prefix := sha
	if len(prefix) > 12 {
		prefix = prefix[:12]
	}
	view := importBatchView{
		ID: batch.ImportBatchID, FileName: batch.FileName, SHA256: sha,
		SHA256Prefix: prefix, Status: batch.Status,
		SourceRowCount: intValue(batch.SourceRowCount), AcceptedRowCount: intValue(batch.AcceptedRowCount),
		ImportedBy: batch.ImportedBy, ImportedAt: batch.ImportedAt.UTC().Format("2006-01-02 15:04 UTC"),
		ReadOnly: batch.Status != domain.ImportStatusUploaded,
	}
	if batch.FailureReason != nil {
		view.FailureReason = *batch.FailureReason
	}
	return view
}

func intValue(value *int) int {
	if value == nil {
		return 0
	}
	return *value
}
