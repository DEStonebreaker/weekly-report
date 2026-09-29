package main

import (
	"embed"
	"log"
	"os"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	if len(os.Args) == 2 && os.Args[1] == "--scheduled-runner" {
		os.Exit(runScheduledRunner())
	}
	app, err := NewApp()
	if err != nil {
		log.Fatal(err)
	}
	if err := wails.Run(&options.App{
		Title: "Weekly Report",
		Width: 1000, Height: 740,
		MinWidth: 640, MinHeight: 540,
		BackgroundColour: &options.RGBA{R: 246, G: 247, B: 251, A: 255},
		AssetServer:      &assetserver.Options{Assets: assets},
		OnStartup:        app.startup,
		Bind:             []interface{}{app},
	}); err != nil {
		log.Fatal(err)
	}
}
