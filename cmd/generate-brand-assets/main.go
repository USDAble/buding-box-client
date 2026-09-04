// generate-brand-assets converts one canonical 1024x1024 PNG into the image
// assets consumed by the current Web, desktop, and packaging pipelines. It has
// no OS-specific external dependency: ICO stores PNG frames and ICNS stores
// standard PNG chunks, so the same command runs on Windows, macOS, and Linux.
package main

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"html"
	"image"
	"image/png"
	"os"
	"path/filepath"

	"golang.org/x/image/draw"
)

var generatedSizes = []int{1024, 512, 256, 192, 180, 152, 144, 128, 120, 96, 72, 64, 48, 32, 16}

func main() {
	input := flag.String("input", "", "path to the canonical 1024x1024 PNG logo")
	flag.Parse()
	if *input == "" || flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "usage: go run ./cmd/generate-brand-assets --input <logo-1024.png>")
		os.Exit(2)
	}
	if err := generateAssets(*input, "."); err != nil {
		fmt.Fprintf(os.Stderr, "generate brand assets: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("Generated Pudding Box platform icon assets. Run: node scripts/sync-branding.mjs")
}

func generateAssets(inputPath, root string) error {
	source, err := os.ReadFile(inputPath)
	if err != nil {
		return fmt.Errorf("read input: %w", err)
	}
	decoded, format, err := image.Decode(bytes.NewReader(source))
	if err != nil {
		return fmt.Errorf("decode input: %w", err)
	}
	if format != "png" {
		return fmt.Errorf("input must be a PNG, got %s", format)
	}
	bounds := decoded.Bounds()
	if bounds.Dx() != 1024 || bounds.Dy() != 1024 {
		return fmt.Errorf("input dimensions must be exactly 1024x1024, got %dx%d", bounds.Dx(), bounds.Dy())
	}
	productName, err := englishProductName(root)
	if err != nil {
		return err
	}

	pngs := make(map[int][]byte, len(generatedSizes))
	for _, size := range generatedSizes {
		encoded, err := renderPNG(decoded, size)
		if err != nil {
			return fmt.Errorf("render %dpx PNG: %w", size, err)
		}
		pngs[size] = encoded
	}

	outputs := make(map[string][]byte, len(generatedSizes)+5)
	for _, size := range generatedSizes {
		outputs[filepath.Join("branding", "assets", fmt.Sprintf("pudding-box-icon-%d.png", size))] = pngs[size]
	}
	outputs[filepath.Join("branding", "assets", "pudding-box-mark.svg")] = embeddedSVG(pngs[1024], productName)

	ico, err := makeICO(pngs, []int{16, 32, 48, 64, 128, 256})
	if err != nil {
		return err
	}
	outputs[filepath.Join("cmd", "octo-desktop", "build", "windows", "icon.ico")] = ico
	outputs[filepath.Join("cmd", "octo-desktop", "build", "linux", "icon.png")] = pngs[256]
	outputs[filepath.Join("cmd", "octo-desktop", "build", "darwin", "tray-icon.png")] = pngs[64]
	outputs[filepath.Join("cmd", "octo-desktop", "build", "darwin", "icon.icns")] = makeICNS(pngs)

	for relativePath, content := range outputs {
		destination := filepath.Join(root, relativePath)
		if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
			return fmt.Errorf("create %s: %w", filepath.Dir(destination), err)
		}
		if err := os.WriteFile(destination, content, 0o644); err != nil {
			return fmt.Errorf("write %s: %w", destination, err)
		}
	}
	return nil
}

func renderPNG(source image.Image, size int) ([]byte, error) {
	if size <= 0 {
		return nil, errors.New("size must be positive")
	}
	destination := image.NewNRGBA(image.Rect(0, 0, size, size))
	draw.CatmullRom.Scale(destination, destination.Bounds(), source, source.Bounds(), draw.Over, nil)
	var encoded bytes.Buffer
	encoder := png.Encoder{CompressionLevel: png.BestCompression}
	if err := encoder.Encode(&encoded, destination); err != nil {
		return nil, err
	}
	return encoded.Bytes(), nil
}

func englishProductName(root string) (string, error) {
	configPath := filepath.Join(root, "branding", "brand.json")
	content, err := os.ReadFile(configPath)
	if err != nil {
		return "", fmt.Errorf("read brand configuration: %w", err)
	}
	var config struct {
		Product struct {
			Names map[string]string `json:"names"`
		} `json:"product"`
	}
	if err := json.Unmarshal(content, &config); err != nil {
		return "", fmt.Errorf("decode brand configuration: %w", err)
	}
	name := config.Product.Names["en-US"]
	if name == "" {
		return "", errors.New("brand configuration has no product.names.en-US value")
	}
	return name, nil
}

func embeddedSVG(canonicalPNG []byte, productName string) []byte {
	payload := base64.StdEncoding.EncodeToString(canonicalPNG)
	title := html.EscapeString(productName)
	return []byte("<svg xmlns=\"http://www.w3.org/2000/svg\" viewBox=\"0 0 1024 1024\" role=\"img\" aria-labelledby=\"title\"><title id=\"title\">" + title + "</title><image width=\"1024\" height=\"1024\" href=\"data:image/png;base64," + payload + "\"/></svg>\n")
}

func makeICO(pngs map[int][]byte, sizes []int) ([]byte, error) {
	for _, size := range sizes {
		if len(pngs[size]) == 0 {
			return nil, fmt.Errorf("ICO frame %dpx is unavailable", size)
		}
	}
	payloadOffset := 6 + len(sizes)*16
	output := bytes.NewBuffer(make([]byte, 0, payloadOffset+len(pngs[256])))
	_ = binary.Write(output, binary.LittleEndian, uint16(0))
	_ = binary.Write(output, binary.LittleEndian, uint16(1))
	_ = binary.Write(output, binary.LittleEndian, uint16(len(sizes)))

	for _, size := range sizes {
		width := byte(size)
		if size == 256 {
			width = 0
		}
		output.WriteByte(width)
		output.WriteByte(width)
		output.WriteByte(0)
		output.WriteByte(0)
		_ = binary.Write(output, binary.LittleEndian, uint16(1))
		_ = binary.Write(output, binary.LittleEndian, uint16(32))
		_ = binary.Write(output, binary.LittleEndian, uint32(len(pngs[size])))
		_ = binary.Write(output, binary.LittleEndian, uint32(payloadOffset))
		payloadOffset += len(pngs[size])
	}
	for _, size := range sizes {
		output.Write(pngs[size])
	}
	return output.Bytes(), nil
}

func makeICNS(pngs map[int][]byte) []byte {
	chunks := []struct {
		kind string
		size int
	}{
		{"icp4", 16}, {"icp5", 32}, {"icp6", 64}, {"ic07", 128}, {"ic08", 256}, {"ic09", 512}, {"ic10", 1024},
		{"ic11", 32}, {"ic12", 64}, {"ic13", 256}, {"ic14", 512},
	}
	body := bytes.NewBuffer(nil)
	for _, chunk := range chunks {
		data := pngs[chunk.size]
		body.WriteString(chunk.kind)
		_ = binary.Write(body, binary.BigEndian, uint32(8+len(data)))
		body.Write(data)
	}
	output := bytes.NewBuffer(make([]byte, 0, 8+body.Len()))
	output.WriteString("icns")
	_ = binary.Write(output, binary.BigEndian, uint32(8+body.Len()))
	output.Write(body.Bytes())
	return output.Bytes()
}
