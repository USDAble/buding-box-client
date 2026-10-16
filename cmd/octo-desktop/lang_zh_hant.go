package main

// OCTO-FORK: native menus and dialogs follow the Traditional Chinese preference.
func zhHantStringsFor(name, short string) uiStrings {
	return uiStrings{
		trayShow:           "顯示" + short,
		trayQuit:           "退出" + short,
		trayNewSession:     "新建會話",
		traySettings:       "設置…",
		trayCheckUpdates:   "檢查更新…",
		trayUpdateAvailFmt: "↑ 更新到 v%s",
		trayStarting:       "啟動中…",
		trayBackendFmt:     "後端 · %s",
		trayClientsFmt:     "已連接客戶端：%d",
		trayChannelsFmt:    "已配置 channel：%d",
		trayPet:            "顯示桌寵",
		trayPetHide:        "隱藏桌寵",

		takeoverTitle:  name,
		takeoverMsgFmt: "已有一個" + name + "後端在後台運行（pid %d）。\n\n停止它，並讓" + name + "作為本機的後端中樞？",
		takeoverOK:     "停止並繼續",
		takeoverCancel: "退出",

		quitTitle:  "退出" + short,
		quitMsg:    "退出會停止本機的" + name + "後端，已連接的編輯器、瀏覽器和 IM channel 都會斷開。\n\n仍要退出？",
		quitOK:     "退出",
		quitCancel: "取消",

		errTitle: name,
		// brand-exception: this prescribed warning names the upstream program occupying the port.
		errBindFmt:    "端口 %s 已被佔用。若本機正在運行 Octo 或其它程序，請先退出後再打開" + name + "。\n\n%v",
		errStopFmt:    "無法停止正在運行的後端：%v",
		errStartFmt:   "無法啟動後端：%v",
		errNoSpaceFmt: "空間不足，無法運行。數據目錄所在磁盤剩餘 %s，需要 %s。\n\n請清理出空間後重新打開" + name + "。",

		otherCopyTitle: name,
		otherCopyMsg:   name + "已在運行，請先退出再打開另一份。",
		dialogOKText:   "好",

		updTitle:         name,
		updFailed:        "檢查更新失敗,請稍後重試。",
		updLatestFmt:     "已是最新版本(v%s)。",
		updAvailableFmt:  name + " %s 已發佈。",
		updOpen:          "打開下載頁",
		updInstall:       "立即更新",
		updInplaceFailed: "自動更新失敗,已打開下載頁。",
	}
}
