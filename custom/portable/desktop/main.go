// Command buding-box-desktop 是便携版专用的最小原生桌面外壳。
//
// 开发边界：本命令只位于 custom/portable，不导入、不修改上游业务包。
// Octo 与 ai-guard 仍由 U 盘启动器作为独立进程管理；本窗口只加载
// http://127.0.0.1:18080，确保 UI、API、WebSocket 和模型流量继续经过
// 外挂代理责任链，而不是绕过网关直连上游服务。
package main

import (
	"log"
	"os"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// shell 查询标记只用于启用上游已经提供的桌面拖拽逻辑，不改变请求地址、
// 鉴权或代理链路；页面和接口仍全部经过本地 ai-guard 网关。
const portableURL = "http://127.0.0.1:18080/?shell=octo-desktop"

// loadApplicationIcon 从启动器指定的便携目录读取应用图标。图标文件由打包层
// 从上游已有桌面资源复制，源码中不重复维护二进制资源；读取失败时交由系统使用
// 默认图标，不影响代理、数据目录和桌面窗口启动。
func loadApplicationIcon() []byte {
	path := os.Getenv("BUDING_BOX_APP_ICON")
	if path == "" {
		return nil
	}
	icon, err := os.ReadFile(path)
	if err != nil {
		log.Printf("便携桌面图标读取失败：%v", err)
		return nil
	}
	return icon
}

func main() {
	// Windows 的 WebView2 数据目录由启动器注入，必须位于 U 盘应用目录。
	// macOS/Linux 的本地化目录分别由 CFFIXED_USER_HOME 和 XDG/HOME 控制。
	webviewData := os.Getenv("BUDING_BOX_WEBVIEW_DATA")

	app := application.New(application.Options{
		Name:        "Buding Box",
		Description: "Buding Box Portable Desktop",
		Icon:        loadApplicationIcon(),
		SingleInstance: &application.SingleInstanceOptions{
			UniqueID: "dev.buding-box.portable.desktop",
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
		Windows: application.WindowsOptions{
			WebviewUserDataPath: webviewData,
			DisabledFeatures:    []string{"CalculateNativeWinOcclusion"},
		},
		Linux: application.LinuxOptions{
			ProgramName: "buding-box-portable",
		},
	})

	app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:     "Buding Box",
		Width:     1280,
		Height:    860,
		MinWidth:  704,
		MinHeight: 480,
		URL:       portableURL,
		// macOS 隐藏原生标题栏并让网页内容延伸到窗口最上方，同时保留系统
		// 红黄绿按钮。按钮避让由 custom/portable/web/portable.css 独立处理，
		// 不修改上游布局组件。
		Mac: application.MacWindow{
			TitleBar: application.MacTitleBarHiddenInset,
		},
		// Windows/Linux 暂时保留系统边框；便携壳不注入上游 NativeBridge，
		// 直接启用无边框会失去可靠的拖动、最小化、最大化和关闭能力。
		Frameless: false,
	})

	if err := app.Run(); err != nil {
		log.Fatalf("便携桌面窗口启动失败：%v", err)
	}
}
