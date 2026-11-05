package main

import (
	"runtime"
	"testing"

	"github.com/open-octo/octo-agent/internal/brand"
	"github.com/open-octo/octo-agent/internal/config"
	"github.com/open-octo/octo-agent/internal/productstate"
	"github.com/wailsapp/wails/v3/pkg/application"
)

func TestNativeMenusFollowSavedLanguage(t *testing.T) {
	t.Setenv("OCTO_DATA_ROOT", t.TempDir())
	prior := active.Load()
	t.Cleanup(func() { active.Store(prior) })
	active.Store(nil)
	app := application.New(application.Options{Name: brand.Load().Name(brand.DefaultLocale)})
	bridge := &nativeBridge{app: app}
	shortcuts := make(map[application.Role]string)
	for _, tc := range []struct {
		locale, file, edit, copy, speak, settings string
	}{
		{"en", "File", "Edit", "Copy", "Start Speaking", "Settings…"},
		{"zh", "文件", "编辑", "复制", "开始朗读", "设置…"},
		{"zh-Hant", "檔案", "編輯", "複製", "開始朗讀", "設置…"},
		{"en", "File", "Edit", "Copy", "Start Speaking", "Settings…"},
	} {
		t.Run(tc.locale, func(t *testing.T) {
			if err := (&config.Config{Language: tc.locale}).Save(); err != nil {
				t.Fatal(err)
			}
			if !applyLang() {
				t.Fatal("changed language did not request a native menu refresh")
			}
			if applyLang() {
				t.Fatal("unchanged language requested another menu refresh")
			}
			tray := buildTrayMenu(app, bridge)
			defer tray.Destroy()
			if tray.FindByLabel(tc.settings) == nil {
				t.Fatalf("tray settings did not follow %s", tc.locale)
			}
			if runtime.GOOS != "darwin" {
				return
			}
			menu := buildAppMenu(app, bridge)
			defer menu.Destroy()
			for role, want := range map[application.Role]string{
				application.FileMenu: tc.file, application.EditMenu: tc.edit,
				application.Copy: tc.copy, application.StartSpeaking: tc.speak,
			} {
				item := menu.FindByRole(role)
				if item == nil || item.Label() != want {
					t.Fatalf("role %d did not follow %s: %v, want %q", role, tc.locale, item, want)
				}
			}
			for _, role := range []application.Role{application.Copy, application.Paste, application.CloseWindow} {
				key := menu.FindByRole(role).GetAccelerator()
				if before, ok := shortcuts[role]; ok && key != before {
					t.Fatalf("language change modified shortcut: %q -> %q", before, key)
				}
				shortcuts[role] = key
			}
			if menu.FindByLabel(L().trayQuit) == nil || menu.FindByRole(application.Quit) != nil {
				t.Fatal("localized menu lost the shell's custom quit action")
			}
			help := menu.FindByRole(application.HelpMenu).GetSubmenu().ItemAt(0)
			if help.Label() != L().menuLearnMore {
				t.Fatal("role-less Help entry was left in English")
			}
		})
	}
}

func TestNativeLanguageUsesProductPreferenceInsteadOfBrowserConfig(t *testing.T) {
	t.Setenv("OCTO_DATA_ROOT", t.TempDir())
	prior := languagePreferences.Load()
	t.Cleanup(func() { languagePreferences.Store(prior) })
	state, err := productstate.Open(productstate.Options{Locale: "zh"})
	if err != nil {
		t.Fatal(err)
	}
	languagePreferences.Store(state)
	for _, language := range []string{"en", "zh-Hant", "zh"} {
		if err := state.SetLocale(language); err != nil {
			t.Fatal(err)
		}
		if err := (&config.Config{Language: "en"}).Save(); err != nil {
			t.Fatal(err)
		}
		if got := resolveLang(); got != language {
			t.Fatalf("native language = %q, product preference = %q", got, language)
		}
	}
}
