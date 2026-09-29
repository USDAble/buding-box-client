package main

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"path/filepath"
	"testing"
)

// OCTO-FORK: phone launchers must keep the desktop mark's source and platform shapes.
func TestMobileIconArtifacts(t *testing.T) {
	source := image.NewNRGBA(image.Rect(0, 0, 1024, 1024))
	for y := 0; y < 1024; y++ {
		for x := 0; x < 1024; x++ {
			source.SetNRGBA(x, y, color.NRGBA{R: 20, G: 80, B: 160, A: 255})
		}
	}

	artifacts := mobileIconArtifacts(source)
	if len(artifacts) != 16 {
		t.Fatalf("got %d launcher assets, want 16", len(artifacts))
	}
	for _, artifact := range artifacts {
		icon, err := png.Decode(bytes.NewReader(artifact.data))
		if err != nil {
			t.Fatalf("decode %s: %v", artifact.path, err)
		}
		name := filepath.Base(artifact.path)
		size := 1024
		if name != "AppIcon-512@2x.png" {
			switch filepath.Base(filepath.Dir(artifact.path)) {
			case "mipmap-mdpi":
				size = 48
			case "mipmap-hdpi":
				size = 72
			case "mipmap-xhdpi":
				size = 96
			case "mipmap-xxhdpi":
				size = 144
			case "mipmap-xxxhdpi":
				size = 192
			default:
				t.Fatalf("unexpected density: %s", artifact.path)
			}
			if name == "ic_launcher_foreground.png" {
				size = size * 9 / 4
			}
		}
		if icon.Bounds().Dx() != size || icon.Bounds().Dy() != size {
			t.Errorf("%s is %v, want %d square", artifact.path, icon.Bounds(), size)
		}
		_, _, _, centerAlpha := icon.At(size/2, size/2).RGBA()
		if centerAlpha != 0xffff {
			t.Errorf("%s lost its centered mark", artifact.path)
		}
		_, _, _, cornerAlpha := icon.At(0, 0).RGBA()
		if name == "ic_launcher_foreground.png" || name == "ic_launcher_round.png" {
			if cornerAlpha != 0 {
				t.Errorf("%s must have transparent corners", artifact.path)
			}
		} else if cornerAlpha != 0xffff {
			t.Errorf("%s must have an opaque background", artifact.path)
		}
	}
}
