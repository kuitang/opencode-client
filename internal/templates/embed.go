package templates

import "embed"

//go:embed templates/*.html templates/tabs/*.html
var TemplateFS embed.FS

//go:embed static/*
var StaticFS embed.FS
