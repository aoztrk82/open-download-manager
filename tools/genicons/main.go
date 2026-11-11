//go:build ignore

// genicons draws the ODM logo and writes every icon the project ships:
//
//	go run tools/genicons/main.go
//
// The logo is a rounded blue square with a down arrow above a bar split into
// segments (parallel connections). Shapes are tested per sub-pixel and
// supersampled, so small sizes stay smooth without an SVG rasterizer.
package main

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"log"
	"math"
	"os"
	"path/filepath"
	"strconv"
)

type rgb struct{ r, g, b float64 }

var (
	top    = rgb{0x3b, 0x82, 0xf6} // blue-500
	bottom = rgb{0x1d, 0x4e, 0xd8} // blue-700
)

func roundRect(x, y, x0, y0, x1, y1, r float64) bool {
	if x < x0 || x > x1 || y < y0 || y > y1 {
		return false
	}
	cx := math.Max(x0+r, math.Min(x, x1-r))
	cy := math.Max(y0+r, math.Min(y, y1-r))
	return (x-cx)*(x-cx)+(y-cy)*(y-cy) <= r*r
}

func inTriangle(px, py float64, a, b, c [2]float64) bool {
	s := func(p1, p2 [2]float64) float64 {
		return (px-p2[0])*(p1[1]-p2[1]) - (p1[0]-p2[0])*(py-p2[1])
	}
	d1, d2, d3 := s(a, b), s(b, c), s(c, a)
	neg := d1 < 0 || d2 < 0 || d3 < 0
	pos := d1 > 0 || d2 > 0 || d3 > 0
	return !(neg && pos)
}

// glyph reports whether (x, y) in unit coordinates is white (arrow / bar).
func glyph(x, y float64, size int) bool {
	// arrow
	if roundRect(x, y, 0.425, 0.17, 0.575, 0.50, 0.03) {
		return true
	}
	if inTriangle(x, y, [2]float64{0.25, 0.44}, [2]float64{0.75, 0.44}, [2]float64{0.50, 0.68}) {
		return true
	}
	// segmented bar: 4 parts, or a solid bar where gaps would be sub-pixel
	y0, y1 := 0.75, 0.84
	if size <= 20 {
		y0, y1 = 0.74, 0.86
	}
	if y < y0 || y > y1 {
		return false
	}
	x0, x1 := 0.20, 0.80
	if size <= 20 {
		return roundRect(x, y, x0, y0, x1, y1, (y1-y0)/2)
	}
	gap := 0.035
	w := (x1 - x0 - 3*gap) / 4
	for i := 0; i < 4; i++ {
		sx := x0 + float64(i)*(w+gap)
		if roundRect(x, y, sx, y0, sx+w, y1, 0.02) {
			return true
		}
	}
	return false
}

func draw(size int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	const ss = 8 // 8x8 samples per pixel
	inset, radius := 0.03, 0.22
	if size <= 20 {
		inset, radius = 0.0, 0.2
	}
	for py := 0; py < size; py++ {
		for px := 0; px < size; px++ {
			var bg, fg int
			for sy := 0; sy < ss; sy++ {
				for sx := 0; sx < ss; sx++ {
					x := (float64(px) + (float64(sx)+0.5)/ss) / float64(size)
					y := (float64(py) + (float64(sy)+0.5)/ss) / float64(size)
					if !roundRect(x, y, inset, inset, 1-inset, 1-inset, radius) {
						continue
					}
					if glyph(x, y, size) {
						fg++
					} else {
						bg++
					}
				}
			}
			n := float64(ss * ss)
			cover := float64(bg+fg) / n
			if cover == 0 {
				continue
			}
			t := (float64(py) + 0.5) / float64(size)
			b := rgb{top.r + (bottom.r-top.r)*t, top.g + (bottom.g-top.g)*t, top.b + (bottom.b-top.b)*t}
			f := float64(fg) / float64(bg+fg) // white share inside the shape
			c := rgb{b.r + (255-b.r)*f, b.g + (255-b.g)*f, b.b + (255-b.b)*f}
			img.SetNRGBA(px, py, color.NRGBA{uint8(c.r + .5), uint8(c.g + .5), uint8(c.b + .5), uint8(cover*255 + .5)})
		}
	}
	return img
}

func pngBytes(size int) []byte {
	var b bytes.Buffer
	if err := png.Encode(&b, draw(size)); err != nil {
		log.Fatal(err)
	}
	return b.Bytes()
}

func write(path string, data []byte) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		log.Fatal(err)
	}
	log.Println("wrote", path)
}

// ico packs PNG images into a Windows .ico (PNG entries are valid since Vista).
func ico(sizes ...int) []byte {
	var imgs [][]byte
	for _, s := range sizes {
		imgs = append(imgs, pngBytes(s))
	}
	var b bytes.Buffer
	binary.Write(&b, binary.LittleEndian, []uint16{0, 1, uint16(len(sizes))})
	off := 6 + 16*len(sizes)
	for i, s := range sizes {
		d := byte(s)
		if s >= 256 {
			d = 0
		}
		b.Write([]byte{d, d, 0, 0})
		binary.Write(&b, binary.LittleEndian, []uint16{1, 32})
		binary.Write(&b, binary.LittleEndian, []uint32{uint32(len(imgs[i])), uint32(off)})
		off += len(imgs[i])
	}
	for _, d := range imgs {
		b.Write(d)
	}
	return b.Bytes()
}

func main() {
	for _, s := range []int{16, 32, 48, 96, 128} {
		write(filepath.Join("extension", "static", "icons", "icon-"+strconv.Itoa(s)+".png"), pngBytes(s))
	}
	write(filepath.Join("internal", "web", "static", "icon.png"), pngBytes(64))
	write(filepath.Join("internal", "web", "static", "favicon.ico"), ico(16, 32, 48))
	write(filepath.Join("cmd", "odm", "assets", "odm.ico"), ico(16, 20, 24, 32, 40, 48, 64, 256))
	write(filepath.Join("cmd", "odm", "assets", "tray.ico"), ico(16, 20, 24, 32, 48))
	write(filepath.Join("cmd", "odm", "assets", "icon-256.png"), pngBytes(256))
}
