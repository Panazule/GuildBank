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
