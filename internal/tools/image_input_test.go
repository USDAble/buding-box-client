package tools

import (
	"context"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/open-octo/octo-agent/internal/agent"
)

func TestReadImageUnverifiedCombinationReportsMetadataWithoutInjectingImage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "poster.png")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(file, image.NewRGBA(image.Rect(0, 0, 24, 36))); err != nil {
		t.Fatal(err)
	}
	file.Close()
	original, _ := os.ReadFile(path)
	for _, allowed := range []bool{false, true} {
		ctx := agent.WithImageInputPolicy(WithModelVision(context.Background(), !allowed), func(context.Context) agent.ImageInputStatus {
			return agent.ImageInputStatus{Known: true, Allowed: allowed, Reason: "capability_not_verified"}
		})
		result, err := (ReadFileTool{}).Execute(ctx, "read_file", map[string]any{"path": path})
		if err != nil {
			t.Fatal(err)
		}
		if ImagesAllowed(ctx) != allowed {
			t.Fatal("local model heuristic overrode signed evidence")
		}
		if allowed {
			if len(result.Blocks) != 1 || result.Blocks[0].Type != "image" {
				t.Fatal("verified image was removed")
			}
		} else {
			if len(result.Blocks) != 0 {
				t.Fatal("unverified image injected into history")
			}
			for _, expected := range []string{"24x36", "NOT performed", "capability_not_verified", "file is unchanged"} {
				if !strings.Contains(result.Text, expected) {
					t.Fatalf("missing %q in %s", expected, result.Text)
				}
			}
		}
	}
	stored, _ := os.ReadFile(path)
	if string(original) != string(stored) {
		t.Fatal("inspection changed deliverable")
	}
}
