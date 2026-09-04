package main

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestGenerateAssetsCreatesPlatformAssetsFrom1024PNG(t *testing.T) {
	root := t.TempDir()
	input := filepath.Join(root, "input.png")
	writeTestPNG(t, input, 1024)
	writeTestBrandConfig(t, root, "Test Pudding & Co")

	if err := generateAssets(input, root); err != nil {
		t.Fatalf("generateAssets() error = %v", err)
	}

	for _, size := range []int{1024, 512, 256, 192, 180, 152, 144, 128, 120, 96, 72, 64, 48, 32, 16} {
		assertPNGSize(t, filepath.Join(root, "branding", "assets", "pudding-box-icon-"+strconv.Itoa(size)+".png"), size)
	}

	mark, err := os.ReadFile(filepath.Join(root, "branding", "assets", "pudding-box-mark.svg"))
	if err != nil {
		t.Fatalf("read generated SVG: %v", err)
	}
	if !bytes.Contains(mark, []byte("data:image/png;base64,")) {
		t.Fatalf("generated SVG does not embed the canonical PNG")
	}
	if !bytes.Contains(mark, []byte("<title id=\"title\">Test Pudding &amp; Co</title>")) {
		t.Fatalf("generated SVG title does not use the configured, escaped product name: %s", mark)
	}

	assertICOImageSizes(t, filepath.Join(root, "cmd", "octo-desktop", "build", "windows", "icon.ico"), []int{16, 32, 48, 64, 128, 256})
	assertPNGSize(t, filepath.Join(root, "cmd", "octo-desktop", "build", "linux", "icon.png"), 256)
	assertPNGSize(t, filepath.Join(root, "cmd", "octo-desktop", "build", "darwin", "tray-icon.png"), 64)
	assertICNS(t, filepath.Join(root, "cmd", "octo-desktop", "build", "darwin", "icon.icns"))
}

func TestGenerateAssetsRejectsNon1024InputWithoutWritingAssets(t *testing.T) {
	root := t.TempDir()
	input := filepath.Join(root, "input.png")
	writeTestPNG(t, input, 512)

	err := generateAssets(input, root)
	if err == nil || !strings.Contains(err.Error(), "1024x1024") {
		t.Fatalf("generateAssets() error = %v, want 1024x1024 validation error", err)
	}
	if _, statErr := os.Stat(filepath.Join(root, "branding", "assets", "pudding-box-icon-1024.png")); !os.IsNotExist(statErr) {
		t.Fatalf("generator wrote output after invalid input; stat error = %v", statErr)
	}
}

func writeTestBrandConfig(t *testing.T, root, englishName string) {
	t.Helper()
	destination := filepath.Join(root, "branding", "brand.json")
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		t.Fatalf("create branding directory: %v", err)
	}
	content := "{\n  \"product\": {\n    \"names\": {\n      \"en-US\": \"" + englishName + "\"\n    }\n  }\n}\n"
	if err := os.WriteFile(destination, []byte(content), 0o644); err != nil {
		t.Fatalf("write brand config: %v", err)
	}
}

func writeTestPNG(t *testing.T, destination string, size int) {
	t.Helper()
	imageData := image.NewNRGBA(image.Rect(0, 0, size, size))
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			imageData.SetNRGBA(x, y, color.NRGBA{R: uint8(x % 256), G: uint8(y % 256), B: 0x80, A: 0xff})
		}
	}
	file, err := os.Create(destination)
	if err != nil {
		t.Fatalf("create %s: %v", destination, err)
	}
	defer file.Close()
	if err := png.Encode(file, imageData); err != nil {
		t.Fatalf("encode input PNG: %v", err)
	}
}

func assertPNGSize(t *testing.T, filename string, want int) {
	t.Helper()
	file, err := os.Open(filename)
	if err != nil {
		t.Fatalf("open %s: %v", filename, err)
	}
	defer file.Close()
	decoded, err := png.Decode(file)
	if err != nil {
		t.Fatalf("decode %s: %v", filename, err)
	}
	if got := decoded.Bounds().Dx(); got != want || decoded.Bounds().Dy() != want {
		t.Fatalf("%s dimensions = %dx%d, want %dx%d", filename, got, decoded.Bounds().Dy(), want, want)
	}
}

func assertICOImageSizes(t *testing.T, filename string, want []int) {
	t.Helper()
	data, err := os.ReadFile(filename)
	if err != nil {
		t.Fatalf("read ICO: %v", err)
	}
	if len(data) < 6 || binary.LittleEndian.Uint16(data[2:4]) != 1 {
		t.Fatalf("%s is not an ICO file", filename)
	}
	count := int(binary.LittleEndian.Uint16(data[4:6]))
	if count != len(want) {
		t.Fatalf("ICO image count = %d, want %d", count, len(want))
	}
	for index, size := range want {
		offset := 6 + index*16
		got := int(data[offset])
		if got == 0 {
			got = 256
		}
		if got != size || int(data[offset+1]) != (size%256) {
			t.Fatalf("ICO frame %d size = %d x %d, want %d x %d", index, got, data[offset+1], size, size)
		}
	}
}

func assertICNS(t *testing.T, filename string) {
	t.Helper()
	data, err := os.ReadFile(filename)
	if err != nil {
		t.Fatalf("read ICNS: %v", err)
	}
	if len(data) < 8 || string(data[:4]) != "icns" || int(binary.BigEndian.Uint32(data[4:8])) != len(data) {
		t.Fatalf("%s is not a well-formed ICNS container", filename)
	}
	for offset := 8; offset < len(data); {
		if offset+8 > len(data) {
			t.Fatalf("truncated ICNS chunk at byte %d", offset)
		}
		chunkLength := int(binary.BigEndian.Uint32(data[offset+4 : offset+8]))
		if chunkLength < 8 || offset+chunkLength > len(data) {
			t.Fatalf("invalid ICNS chunk at byte %d", offset)
		}
		offset += chunkLength
	}
}
