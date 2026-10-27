package main

import (
	"context"
	"net/http"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/menu"
	"github.com/wailsapp/wails/v2/pkg/menu/keys"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// runWindow shows the admin UI in a native window and blocks until the user
// quits from the menu. start runs once, when the window opens. Closing the
// window only hides it, so mail keeps flowing; opening Mailer again shows
// it. started reports whether start ran: when the binary was built without
// the window (no "desktop" build tag), runWindow fails before it does.
func runWindow(admin http.Handler, start func() error) (started bool, err error) {
	var app context.Context
	var startErr error

	bar := menu.NewMenu()
	bar.AddSubmenu("Mailer").AddText("Quit Mailer", keys.CmdOrCtrl("q"), func(*menu.CallbackData) {
		runtime.Quit(app)
	})

	err = wails.Run(&options.App{
		Title:             "Mailer",
		Width:             1280,
		Height:            820,
		MinWidth:          960,
		MinHeight:         600,
		HideWindowOnClose: true,
		Menu:              bar,
		AssetServer:       &assetserver.Options{Handler: admin},
		SingleInstanceLock: &options.SingleInstanceLock{
			UniqueId: "c2b7e0d4-5f1a-4f0e-9a43-mailer-window",
			OnSecondInstanceLaunch: func(options.SecondInstanceData) {
				runtime.WindowUnminimise(app)
				runtime.WindowShow(app)
			},
		},
		OnStartup: func(ctx context.Context) {
			app, started = ctx, true
			if startErr = start(); startErr != nil {
				runtime.MessageDialog(ctx, runtime.MessageDialogOptions{
					Type: runtime.ErrorDialog, Title: "Mailer can't start", Message: startErr.Error(),
				})
				runtime.Quit(ctx)
			}
		},
	})
	if err == nil {
		err = startErr
	}
	return started, err
}
