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

const portableURL = "http://127.0.0.1:18080"

func main() {
	// Windows 的 WebView2 数据目录由启动器注入，必须位于 U 盘应用目录。
	// macOS/Linux 的本地化目录分别由 CFFIXED_USER_HOME 和 XDG/HOME 控制。
	webviewData := os.Getenv("BUDING_BOX_WEBVIEW_DATA")

	app := application.New(application.Options{
		Name:        "Buding Box",
		Description: "Buding Box Portable Desktop",
		SingleInstance: &application.SingleInstanceOptions{
			UniqueID: "dev.buding-box.portable.desktop",
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
		Windows: application.WindowsOptions{
			WebviewUserDataPath: webviewData,
			DisabledFeatures:   []string{"CalculateNativeWinOcclusion"},
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
		// 使用各系统标准标题栏。便携壳不注入上游 NativeBridge，标准标题栏
		// 可直接提供可靠的拖动、最小化、最大化和关闭能力。
		Frameless: false,
	})

	if err := app.Run(); err != nil {
		log.Fatalf("便携桌面窗口启动失败：%v", err)
	}
}
