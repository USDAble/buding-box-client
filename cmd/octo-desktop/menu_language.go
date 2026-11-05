package main

import "github.com/wailsapp/wails/v3/pkg/application"

// Framework roles retain their native actions and accelerators; only their labels use app copy.
func localizeAppMenu(menu *application.Menu) {
	copy := L()
	for role, label := range map[application.Role]string{
		application.About:              copy.menuAbout,
		application.ServicesMenu:       copy.menuServices,
		application.Hide:               copy.menuHide,
		application.HideOthers:         copy.menuHideOthers,
		application.UnHide:             copy.menuShowAll,
		application.FileMenu:           copy.menuFile,
		application.EditMenu:           copy.menuEdit,
		application.ViewMenu:           copy.menuView,
		application.WindowMenu:         copy.menuWindow,
		application.HelpMenu:           copy.menuHelp,
		application.CloseWindow:        copy.menuClose,
		application.Undo:               copy.menuUndo,
		application.Redo:               copy.menuRedo,
		application.Cut:                copy.menuCut,
		application.Copy:               copy.menuCopy,
		application.Paste:              copy.menuPaste,
		application.PasteAndMatchStyle: copy.menuPasteStyle,
		application.Delete:             copy.menuDelete,
		application.SelectAll:          copy.menuSelectAll,
		application.SpeechMenu:         copy.menuSpeech,
		application.StartSpeaking:      copy.menuStartSpeaking,
		application.StopSpeaking:       copy.menuStopSpeaking,
		application.Reload:             copy.menuReload,
		application.ForceReload:        copy.menuForceReload,
		application.ResetZoom:          copy.menuActualSize,
		application.ZoomIn:             copy.menuZoomIn,
		application.ZoomOut:            copy.menuZoomOut,
		application.ToggleFullscreen:   copy.menuFullscreen,
		application.OpenDevTools:       copy.menuDevTools,
		application.Minimise:           copy.menuMinimise,
		application.Zoom:               copy.menuZoom,
		application.Front:              copy.menuFront,
	} {
		if item := menu.FindByRole(role); item != nil {
			item.SetLabel(label)
		}
	}
	// The framework's Help entry has no role; preserve its callback when translating it.
	if help := menu.FindByRole(application.HelpMenu); help != nil {
		if item := help.GetSubmenu().ItemAt(0); item != nil {
			item.SetLabel(copy.menuLearnMore)
		}
	}
}
