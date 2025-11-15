package server

import (
	"fmt"
	"html/template"
	"net/http"
	"path/filepath"

	"loopfs/pkg/log"

	"github.com/labstack/echo/v4"
)

func (cas *CASServer) serveSwaggerUI(ctx echo.Context) error {
	tmplPath := filepath.Join(cas.webDir, "swagger-ui.html")
	tmpl, err := template.ParseFiles(tmplPath)
	if err != nil {
		log.Error().Err(err).Str("template_path", tmplPath).Msg("Failed to load template")
		return ctx.String(http.StatusInternalServerError, fmt.Sprintf("Failed to load template: %v", err))
	}

	data := struct {
		Title       string
		SwaggerPath string
	}{
		Title:       "CAS Server API Documentation",
		SwaggerPath: "/swagger.yml",
	}

	ctx.Response().Header().Set(echo.HeaderContentType, echo.MIMETextHTMLCharsetUTF8)
	ctx.Response().WriteHeader(http.StatusOK)
	return tmpl.Execute(ctx.Response().Writer, data)
}

func (cas *CASServer) serveSwaggerSpec(ctx echo.Context) error {
	swaggerPath := filepath.Join(cas.webDir, "swagger.yml")
	return ctx.File(swaggerPath)
}
