// Command generate-brand-assets turns the brand master under branding/source/
// into every bitmap the product ships: the Windows icon.ico, the macOS
// icon.icns + tray-icon.png, the Linux icon.png, and the landing og-image.png.
// This is the "PR2" tool from dev-docs-usdable/需求/2260906/品牌升级方案.md
// §5.2; it needs no npm because rendering is pure Go (oksvg + rasterx for
// vector, image/png for raster).
//
// Single-master model (2026-09-09): the designer delivers ONE high-resolution
// PNG, logo-mark.png (the coloured master), and the rest is derived here:
//
//   - logo-mark.png   → every square icon (ICO / ICNS / Linux PNG). Required.
//   - logo-mono        → the macOS menu-bar template. Optional source file;
//     when absent it is derived from the master's alpha as a black silhouette
//     (SetTemplateIcon reads alpha only). A hand-cut silhouette with true
//     holes is still preferred when the master has detail only in colour.
//   - og-card          → the 1200×630 social card. Optional; needs layout and
//     typography, so it is never synthesised — ship no og-card.png and the
//     previously generated og-image.png is left untouched.
//
// Each logical source resolves to either a .svg (validated vector; still
// supported if true vectors land later) or a .png (the standard raster path).
// The .svg path enforces 品牌资产交付说明 §2: a fake vector (embedded
// <image>/base64, or live <text>) fails the build instead of shipping a
// degraded icon. When a dedicated logo-mono source exists it must be a pure
// #000000 silhouette; a coloured file fails instead of degrading the tray.
//
// Usage:
//
//	go run ./cmd/generate-brand-assets                 render + write
//	go run ./cmd/generate-brand-assets --check         re-render, byte-compare, fail on drift
//	go run ./cmd/generate-brand-assets --source DIR    override the source dir (default branding/source)
//
// Exit code 0 on success; 1 on a contract violation or a drifted artifact.
package main

import (
	"bytes"
	"encoding/binary"
	"errors"
	"flag"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/srwiley/oksvg"
	"github.com/srwiley/rasterx"
	"golang.org/x/image/draw"
)

// sourceSpec maps a logical asset to its contract viewBox (SVG sources only)
// and its rendered raster size in pixels. logo-mark is the only required
// source; logo-mono and og-card are optional (see file header — mono is
// derived from the master when absent, og-card is skipped).
type sourceSpec struct {
	name     string
	viewBox  [4]float64
	width    int
	height   int
	mustMono bool // a dedicated logo-mono source must be a pure #000000 silhouette (macOS template)
}

var sources = []sourceSpec{
	{name: "logo-mark", viewBox: [4]float64{0, 0, 100, 100}, width: 1024, height: 1024},
	{name: "logo-mono", viewBox: [4]float64{0, 0, 100, 100}, width: 88, height: 88, mustMono: true},
	{name: "og-card", viewBox: [4]float64{0, 0, 1200, 630}, width: 1200, height: 630},
}

func main() {
	sourceDir := flag.String("source", "branding/source", "directory holding the brand sources")
	check := flag.Bool("check", false, "re-render and byte-compare against committed artifacts; do not write")
	flag.Parse()

	if err := run(*sourceDir, *check); err != nil {
		fmt.Fprintln(os.Stderr, "generate-brand-assets:", err)
		os.Exit(1)
	}
}

func run(sourceDir string, checkOnly bool) error {
	artifacts := []artifact{}

	mark, found, err := loadSource(sourceDir, sources[0])
	if err != nil {
		return fmt.Errorf("logo-mark: %w", err)
	}
	if !found {
		return errors.New("logo-mark source missing — put branding/source/logo-mark.png (the coloured master, ≥2048×2048, transparent around the logo) in place and re-run")
	}
	artifacts = append(artifacts, markArtifacts(mark)...)

	mono, found, err := loadSource(sourceDir, sources[1])
	if err != nil {
		return fmt.Errorf("logo-mono: %w", err)
	}
	if !found {
		// Single-master fallback: the macOS template channel reads alpha only,
		// so the master's alpha copied onto pure black is a valid silhouette.
		mono = deriveMono(mark, sources[1].width)
		fmt.Println("note: no dedicated logo-mono source — tray silhouette derived from logo-mark (see 品牌资产交付说明 §3)")
	}
	artifacts = append(artifacts, monoArtifacts(mono)...)

	og, found, err := loadSource(sourceDir, sources[2])
	if err != nil {
		return fmt.Errorf("og-card: %w", err)
	}
	if !found {
		// og-card needs layout + typography, so it is never synthesised; it is
		// a later design deliverable (品牌资产交付说明 §1 第二批 — non-blocking).
		fmt.Println("note: og-card source missing — og-image.png not regenerated")
	} else {
		artifacts = append(artifacts, ogArtifacts(og)...)
	}

	for _, a := range artifacts {
		if checkOnly {
			existing, err := os.ReadFile(a.path)
			if err != nil {
				return fmt.Errorf("check %s: missing committed artifact: %w", a.path, err)
			}
			if !bytes.Equal(existing, a.data) {
				return fmt.Errorf("check %s: rendered bytes differ from committed file — run `go run ./cmd/generate-brand-assets`", a.path)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(a.path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(a.path, a.data, 0o644); err != nil {
			return fmt.Errorf("write %s: %w", a.path, err)
		}
		fmt.Printf("wrote %s (%d bytes)\n", a.path, len(a.data))
	}
	if checkOnly {
		fmt.Println("all rendered artifacts match the committed files.")
	}
	return nil
}

type artifact struct {
	path string
	data []byte
}

// loadSource resolves a logical name to .svg (vector) or .png (raster) and
// returns an image scaled to the spec's rendered size. found is false when
// neither file exists under the given name; any other failure (decode error,
// contract violation) is returned as err so optional assets can distinguish
// "designer has not delivered it yet" from "designer delivered something bad".
func loadSource(sourceDir string, spec sourceSpec) (*image.NRGBA, bool, error) {
	for _, ext := range []string{".svg", ".png"} {
		p := filepath.Join(sourceDir, spec.name+ext)
		data, err := os.ReadFile(p)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, false, err
		}
		if ext == ".svg" {
			if err := validateSVG(spec, data); err != nil {
				return nil, false, err
			}
			img, err := renderSVG(data, spec.width, spec.height)
			return img, true, err
		}
		img, err := loadPNG(spec, data)
		return img, true, err
	}
	return nil, false, nil
}

// ── vector path ────────────────────────────────────────────────────────────

// validateSVG enforces 品牌资产交付说明 §2.1 (no live text, no embedded bitmap)
// and the per-file viewBox. oksvg silently skips <image>/<text>, which would
// ship a degraded icon, so both are hard failures.
func validateSVG(spec sourceSpec, data []byte) error {
	lower := strings.ToLower(string(data))
	if strings.Contains(lower, "<text") {
		return errors.New("contains live <text> — outline all text before export (文字转曲)")
	}
	if strings.Contains(lower, "<image") || strings.Contains(lower, "base64") {
		return errors.New("contains an embedded bitmap (<image>/base64) — export the logo as pure vector paths only, no raster layers")
	}
	if spec.mustMono && !isPureMonochromeSVG(data) {
		return errors.New("logo-mono must use only #000000 fills — see 品牌资产交付说明 §3")
	}
	vb := viewBoxOf(data)
	if vb != spec.viewBox {
		return fmt.Errorf("viewBox %v does not match required %v", vb, spec.viewBox)
	}
	return nil
}

// isPureMonochromeSVG rejects any fill/stroke colour that is not #000000 (or
// the transparent/none/inherit set that carries no colour). It is a cheap
// textual check; the rendered output is what actually matters, but catching it
// at the source gives a clearer message than a muddy raster.
func isPureMonochromeSVG(data []byte) bool {
	// Walk fill="…"/stroke="…" attribute values and require black/transparent.
	s := string(data)
	allowed := map[string]bool{"#000000": true, "black": true, "none": true, "transparent": true, "currentColor": true}
	for _, attr := range []string{"fill=", "stroke=", "stop-color="} {
		rest := s
		for {
			i := strings.Index(rest, attr)
			if i < 0 {
				break
			}
			rest = rest[i+len(attr):]
			if len(rest) == 0 {
				break
			}
			q := rest[0]
			var val string
			if q == '"' || q == '\'' {
				end := strings.IndexByte(rest[1:], q)
				if end < 0 {
					break
				}
				val = rest[1 : 1+end]
				rest = rest[1+end+1:]
			} else {
				end := strings.IndexAny(rest, " />\t\n")
				if end < 0 {
					end = len(rest)
				}
				val = rest[:end]
				rest = rest[end:]
			}
			val = strings.TrimSpace(strings.ToLower(val))
			if strings.HasPrefix(val, "url(") || strings.HasPrefix(val, "rgb") {
				return false
			}
			if val != "" && !allowed[val] {
				return false
			}
		}
	}
	return true
}

func viewBoxOf(data []byte) [4]float64 {
	marker := []byte(`viewBox="`)
	idx := bytes.Index(data, marker)
	if idx < 0 {
		return [4]float64{}
	}
	rest := string(data[idx+len(marker):])
	end := strings.IndexByte(rest, '"')
	if end < 0 {
		return [4]float64{}
	}
	var vb [4]float64
	fields := strings.Fields(rest[:end])
	for i := 0; i < len(fields) && i < 4; i++ {
		fmt.Sscanf(fields[i], "%g", &vb[i])
	}
	return vb
}

// renderSVG rasterizes an SVG into a w×h canvas, preserving aspect ratio and
// letterboxing (transparent padding) so a non-square source is centered.
func renderSVG(data []byte, w, h int) (*image.NRGBA, error) {
	icon, err := oksvg.ReadIconStream(bytes.NewReader(data), oksvg.IgnoreErrorMode)
	if err != nil {
		return nil, err
	}
	vw, vh := icon.ViewBox.W, icon.ViewBox.H
	if vw <= 0 || vh <= 0 {
		return nil, errors.New("viewBox has zero width or height")
	}
	return rasterize(icon, vw, vh, w, h)
}

// ── raster path ────────────────────────────────────────────────────────────

// maxPNGSourceDim caps how large a raster source may be. Design masters are
// 2048–4096px; a Figma export at 12000² decodes to ~0.5 GiB of RAM in the
// pipeline for no quality gain (every consumer is ≤1024px). Downscale once
// when ingesting instead of committing multi-GB-memory files.
const maxPNGSourceDim = 4096

// loadPNG decodes a PNG source and scales it to the spec's rendered size.
func loadPNG(spec sourceSpec, data []byte) (*image.NRGBA, error) {
	cfg, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("decode PNG header: %w", err)
	}
	if cfg.Width > maxPNGSourceDim || cfg.Height > maxPNGSourceDim {
		return nil, fmt.Errorf("source is %d×%d — above the %dpx cap; downscale to 2048–%d when ingesting (see 品牌资产交付说明 §1)", cfg.Width, cfg.Height, maxPNGSourceDim, maxPNGSourceDim)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("decode PNG: %w", err)
	}
	if spec.mustMono {
		if err := checkPureBlack(img); err != nil {
			return nil, err
		}
	}
	return fitTo(img, spec.width, spec.height), nil
}

// checkPureBlack enforces 品牌资产交付说明 §3 on a raster mono source: the only
// non-transparent colour may be #000000 (the macOS template channel drops
// colour anyway, but a coloured source means the designer delivered the wrong
// asset).
func checkPureBlack(img image.Image) error {
	var colored int
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r, g, bl, a := img.At(x, y).RGBA()
			if a < 0x1000 { // transparent enough to ignore
				continue
			}
			if r > 0x0fff || g > 0x0fff || bl > 0x0fff {
				colored++
			}
		}
	}
	if colored > 0 {
		return fmt.Errorf("logo-mono must be a pure #000000 silhouette; found %d non-black opaque pixels", colored)
	}
	return nil
}

// deriveMono builds the macOS menu-bar template from the coloured master when
// no dedicated logo-mono source exists. SetTemplateIcon reads the alpha
// channel only, so copying alpha onto pure black yields the outer silhouette.
// Limitation: detail drawn in lighter colour *inside* an opaque body (eyes,
// mouth) is not in the alpha channel and is lost at menu-bar size — a
// hand-cut logo-mono.png with real holes is still the better tray glyph, and
// the pipeline prefers it whenever the file is present.
func deriveMono(mark *image.NRGBA, size int) *image.NRGBA {
	small := downscale(mark, size)
	out := image.NewNRGBA(image.Rect(0, 0, size, size))
	for i := 0; i+3 < len(small.Pix); i += 4 {
		out.Pix[i+0], out.Pix[i+1], out.Pix[i+2] = 0, 0, 0
		out.Pix[i+3] = small.Pix[i+3]
	}
	return out
}

// fitTo scales src to fit within tw×th, preserving aspect and centering on a
// transparent canvas.
func fitTo(src image.Image, tw, th int) *image.NRGBA {
	sb := src.Bounds()
	sw, sh := sb.Dx(), sb.Dy()
	scale := minf(float64(tw)/float64(sw), float64(th)/float64(sh))
	rw := int(float64(sw)*scale + 0.5)
	rh := int(float64(sh)*scale + 0.5)
	if rw < 1 {
		rw = 1
	}
	if rh < 1 {
		rh = 1
	}
	scaled := image.NewNRGBA(image.Rect(0, 0, rw, rh))
	draw.CatmullRom.Scale(scaled, scaled.Bounds(), src, sb, draw.Src, nil)
	dst := image.NewNRGBA(image.Rect(0, 0, tw, th))
	ox := (tw - rw) / 2
	oy := (th - rh) / 2
	draw.Draw(dst, image.Rect(ox, oy, ox+rw, oy+rh), scaled, image.Point{}, draw.Src)
	return dst
}

func minf(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

// ── shared rasterizer for the vector path ──────────────────────────────────

// rasterize draws icon (with the given viewBox dims) into w×h, letterboxed.
func rasterize(icon *oksvg.SvgIcon, vw, vh float64, w, h int) (*image.NRGBA, error) {
	scale := minf(float64(w)/vw, float64(h)/vh)
	rw := int(vw*scale + 0.5)
	rh := int(vh*scale + 0.5)
	if rw < 1 {
		rw = 1
	}
	if rh < 1 {
		rh = 1
	}
	src := image.NewNRGBA(image.Rect(0, 0, rw, rh))
	icon.SetTarget(0, 0, float64(rw), float64(rh))
	scanner := rasterx.NewScannerGV(rw, rh, src, src.Bounds())
	dasher := rasterx.NewDasher(rw, rh, scanner)
	icon.Draw(dasher, 1.0)

	dst := image.NewNRGBA(image.Rect(0, 0, w, h))
	ox := (w - rw) / 2
	oy := (h - rh) / 2
	draw.Draw(dst, image.Rect(ox, oy, ox+rw, oy+rh), src, image.Point{}, draw.Src)
	return dst, nil
}

// downscale renders src into a size×size image via CatmullRom.
func downscale(src *image.NRGBA, size int) *image.NRGBA {
	dst := image.NewNRGBA(image.Rect(0, 0, size, size))
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, src.Bounds(), draw.Src, nil)
	return dst
}

// ── encoders ───────────────────────────────────────────────────────────────

func pngBytes(img *image.NRGBA) []byte {
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		panic(err) // encoding an in-memory NRGBA cannot fail
	}
	return buf.Bytes()
}

// icoBytes assembles a multi-frame ICO (PNG-compressed frames). A 256px frame
// is encoded as width 0 in its directory entry per the ICO spec.
func icoBytes(sizes []int, frames map[int][]byte) []byte {
	sorted := append([]int(nil), sizes...)
	sort.Ints(sorted)

	var buf bytes.Buffer
	buf.Write([]byte{0, 0, 1, 0}) // reserved + type: icon
	binary.Write(&buf, binary.LittleEndian, uint16(len(sorted)))
	off := 6 + 16*len(sorted)
	for _, s := range sorted {
		data := frames[s]
		buf.WriteByte(byte(s))                                     // width (256 wraps to 0)
		buf.WriteByte(byte(s))                                     // height
		buf.WriteByte(0)                                           // color count
		buf.WriteByte(0)                                           // reserved
		binary.Write(&buf, binary.LittleEndian, uint16(1))         // planes
		binary.Write(&buf, binary.LittleEndian, uint16(32))        // bpp
		binary.Write(&buf, binary.LittleEndian, uint32(len(data))) // bytes in resource
		binary.Write(&buf, binary.LittleEndian, uint32(off))       // offset
		off += len(data)
	}
	for _, s := range sorted {
		buf.Write(frames[s])
	}
	return buf.Bytes()
}

// icnsBytes assembles an ICNS from PNG chunks using the modern ic07…ic14 set
// plus icp4/icp5/icp6 for small sizes.
func icnsBytes(pngs map[string][]byte) []byte {
	order := []string{"icp4", "icp5", "icp6", "ic07", "ic08", "ic09", "ic10", "ic11", "ic12", "ic13", "ic14"}
	var body bytes.Buffer
	for _, t := range order {
		data, ok := pngs[t]
		if !ok {
			continue
		}
		body.WriteString(t)
		binary.Write(&body, binary.BigEndian, uint32(8+len(data)))
		body.Write(data)
	}
	var out bytes.Buffer
	out.WriteString("icns")
	binary.Write(&out, binary.BigEndian, uint32(8+body.Len()))
	out.Write(body.Bytes())
	return out.Bytes()
}

// ── artifact builders ──────────────────────────────────────────────────────

// markArtifacts derives the square-icon set from logo-mark.
func markArtifacts(src *image.NRGBA) []artifact {
	sizes := []int{16, 24, 32, 48, 64, 128, 256, 512, 1024}
	frames := map[int][]byte{}
	for _, s := range sizes {
		frames[s] = pngBytes(downscale(src, s))
	}
	ico := icoBytes([]int{16, 24, 32, 48, 256}, frames)
	icns := icnsBytes(map[string][]byte{
		"icp4": frames[16],
		"icp5": frames[32],
		"icp6": frames[64],
		"ic07": frames[128],
		"ic08": frames[256],
		"ic09": frames[512],
		"ic10": frames[1024],
	})
	// Browser-tab / docs favicon (16/32/48 PNG frames in one .ico) and the
	// in-UI logo the Svelte components load via brandAsset('mark').
	favicon := icoBytes([]int{16, 32, 48}, frames)
	return []artifact{
		{path: filepath.Join("cmd", "octo-desktop", "build", "windows", "icon.ico"), data: ico},
		{path: filepath.Join("cmd", "octo-desktop", "build", "darwin", "icon.icns"), data: icns},
		{path: filepath.Join("cmd", "octo-desktop", "build", "linux", "icon.png"), data: frames[256]},
		{path: filepath.Join("web", "public", "assets", "logo-mark.png"), data: frames[512]},
		{path: filepath.Join("web", "public", "favicon.ico"), data: favicon},
		{path: filepath.Join("docs", "public", "favicon.ico"), data: favicon},
	}
}

// monoArtifacts derives the macOS menu-bar template icon from logo-mono.
func monoArtifacts(src *image.NRGBA) []artifact {
	return []artifact{
		{path: filepath.Join("cmd", "octo-desktop", "build", "darwin", "tray-icon.png"), data: pngBytes(src)},
	}
}

// ogArtifacts derives the 1200×630 social card from og-card.
func ogArtifacts(src *image.NRGBA) []artifact {
	return []artifact{
		{path: filepath.Join("landing", "og-image.png"), data: pngBytes(src)},
	}
}
