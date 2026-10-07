package assets

import (
	_ "embed"
	"embed"
)

//go:embed web/*
var WebFS embed.FS

//go:embed wintun.dll
var WintunDLL []byte

//go:embed app.ico
var AppIcon []byte
