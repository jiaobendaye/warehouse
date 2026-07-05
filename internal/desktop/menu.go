package desktop

import (
	"fmt"
	"os/exec"
	"runtime"

	wailsMenu "github.com/wailsapp/wails/v2/pkg/menu"
	"github.com/wailsapp/wails/v2/pkg/menu/keys"
	wailsRuntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// BuildMenu creates the native application menu for the warehouse desktop
// window. The menu bar exposes a single "操作" dropdown containing the two
// only actions:
//
//   - 在浏览器打开 — opens the configured server address in the system
//     default browser (handy when the user wants the full window chrome).
//   - 刷新 — reloads the embedded WebView (e.g. after editing data
//     outside the GUI).
//
// Wails requires every top-level menu item to be a SubMenu — on Linux/GTK
// bare Text items at the top level are silently dropped, which would leave
// the window with no menu bar at all.
func BuildMenu(app *App) *wailsMenu.Menu {
	openBrowser := func(_ *wailsMenu.CallbackData) {
		url := fmt.Sprintf("http://%s:%d", app.cfg.Host, app.cfg.Port)
		_ = openURL(url)
	}

	reload := func(_ *wailsMenu.CallbackData) {
		if app.ctx == nil {
			return
		}
		wailsRuntime.WindowReload(app.ctx)
	}

	actions := wailsMenu.NewMenuFromItems(
		wailsMenu.Text("在浏览器打开", nil, openBrowser),
		wailsMenu.Separator(),
		wailsMenu.Text("刷新", keys.CmdOrCtrl("r"), reload),
	)

	return wailsMenu.NewMenuFromItems(
		wailsMenu.SubMenu("操作", actions),
	)
}

// openURL opens the given URL in the system's default browser.
func openURL(url string) error {
	switch runtime.GOOS {
	case "linux":
		return exec.Command("xdg-open", url).Start()
	case "darwin":
		return exec.Command("open", url).Start()
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	default:
		return fmt.Errorf("unsupported platform")
	}
}