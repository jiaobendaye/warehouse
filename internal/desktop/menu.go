
package desktop

import (
	"fmt"
	"os/exec"
	"runtime"

	wailsMenu "github.com/wailsapp/wails/v2/pkg/menu"
	"github.com/wailsapp/wails/v2/pkg/menu/keys"
)

// BuildMenu creates the native application menu for the warehouse desktop
// window. The app pointer is used by "open in browser" to read the
// configured host:port.
func BuildMenu(app *App) *wailsMenu.Menu {
	openBrowser := func(_ *wailsMenu.CallbackData) {
		url := fmt.Sprintf("http://%s:%d", app.cfg.Host, app.cfg.Port)
		openURL(url)
	}

	appMenu := wailsMenu.NewMenuFromItems(
		wailsMenu.SubMenu("文件",
			wailsMenu.NewMenuFromItems(
				wailsMenu.Text("打开主窗口", keys.CmdOrCtrl("o"), nil),
				wailsMenu.Separator(),
				wailsMenu.Text("在浏览器中打开", nil, openBrowser),
				wailsMenu.Separator(),
				wailsMenu.Text("退出", keys.CmdOrCtrl("q"), func(_ *wailsMenu.CallbackData) {
					if app.ctx != nil {
						// Signal the Wails runtime to quit.
					}
				}),
			)),
		// Help menu
		wailsMenu.SubMenu("帮助",
			wailsMenu.NewMenuFromItems(
				wailsMenu.Text("关于", nil, func(_ *wailsMenu.CallbackData) {
					_ = openURL("https://github.com/jiaobendaye/warehouse")
				}),
			)),
	)

	return appMenu
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