package build

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"strconv"
	"strings"

	"github.com/stricttools/selfdoc/internal/util"
)

// ogCardWidth and ogCardHeight are the recommended social-card resolution,
// which every generated card carries.
const (
	ogCardWidth  = 1200
	ogCardHeight = 630
)

// GenerateOGPNGBasic generates a 1200x630 PNG social card carrying no text.
//
// The card is written by hand rather than through an image library: a light
// background derived from the accent colour, a 16-pixel accent bar across the
// top, and a striped accent band in the lower portion. accentColor is a
// "#rrggbb" string.
//
// This is the only card path. The Python surface had a second one that drew
// the project name and the page title through an external renderer when it
// happened to be installed, which made a build's output depend on what was in
// the environment; the card a build emits is now the same card everywhere.
func GenerateOGPNGBasic(accentColor string) ([]byte, error) {
	ar, ag, ab, err := parseHexColor(accentColor)
	if err != nil {
		return nil, err
	}

	// Background: the accent at about a tenth of its strength over white.
	bgR := 255 - (255-ar)/10
	bgG := 255 - (255-ag)/10
	bgB := 255 - (255-ab)/10

	accentRow := repeatPixel(ar, ag, ab, ogCardWidth)
	backgroundRow := repeatPixel(bgR, bgG, bgB, ogCardWidth)

	const topBarHeight = 16
	// Eight-pixel accent stripes every forty pixels through the lower band.
	stripeStart := ogCardHeight - 160

	raw := make([]byte, 0, ogCardHeight*(1+ogCardWidth*3))
	for y := 0; y < ogCardHeight; y++ {
		// Each scanline is prefixed with filter byte 0 (no filter).
		raw = append(raw, 0)
		switch {
		case y < topBarHeight:
			raw = append(raw, accentRow...)
		case y >= stripeStart && (y-stripeStart)%40 < 8:
			raw = append(raw, accentRow...)
		default:
			raw = append(raw, backgroundRow...)
		}
	}

	var compressed bytes.Buffer
	writer, err := zlib.NewWriterLevel(&compressed, zlib.BestCompression)
	if err != nil {
		return nil, err
	}
	if _, err := writer.Write(raw); err != nil {
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}

	var ihdr bytes.Buffer
	_ = binary.Write(&ihdr, binary.BigEndian, uint32(ogCardWidth))
	_ = binary.Write(&ihdr, binary.BigEndian, uint32(ogCardHeight))
	// Bit depth 8, colour type 2 (RGB), and the three zero bytes for the
	// compression, filter and interlace methods.
	ihdr.Write([]byte{8, 2, 0, 0, 0})

	var png bytes.Buffer
	png.WriteString("\x89PNG\r\n\x1a\n")
	png.Write(pngChunk("IHDR", ihdr.Bytes()))
	png.Write(pngChunk("IDAT", compressed.Bytes()))
	png.Write(pngChunk("IEND", nil))
	return png.Bytes(), nil
}

// repeatPixel builds one scanline of a single RGB colour.
func repeatPixel(r, g, b, width int) []byte {
	row := make([]byte, 0, width*3)
	for i := 0; i < width; i++ {
		row = append(row, byte(r), byte(g), byte(b))
	}
	return row
}

// pngChunk frames a PNG chunk: its length, its type, its data and the CRC-32
// of the type and data together.
func pngChunk(chunkType string, data []byte) []byte {
	var out bytes.Buffer
	_ = binary.Write(&out, binary.BigEndian, uint32(len(data)))
	body := append([]byte(chunkType), data...)
	out.Write(body)
	_ = binary.Write(&out, binary.BigEndian, crc32.ChecksumIEEE(body))
	return out.Bytes()
}

// parseHexColor reads an "#rrggbb" colour into its three components.
func parseHexColor(color string) (r, g, b int, err error) {
	hex := strings.TrimPrefix(color, "#")
	if len(hex) < 6 {
		return 0, 0, 0, fmt.Errorf("accent colour %q is not a six-digit hex colour", color)
	}
	parse := func(pair string) (int, error) {
		value, convErr := strconv.ParseInt(pair, 16, 32)
		if convErr != nil {
			return 0, fmt.Errorf("accent colour %q is not a six-digit hex colour", color)
		}
		return int(value), nil
	}
	if r, err = parse(hex[0:2]); err != nil {
		return 0, 0, 0, err
	}
	if g, err = parse(hex[2:4]); err != nil {
		return 0, 0, 0, err
	}
	if b, err = parse(hex[4:6]); err != nil {
		return 0, 0, 0, err
	}
	return r, g, b, nil
}

// GenerateFaviconSVG generates a favicon from the project name's initial, on
// the accent colour.
//
// A project with an empty name gets "D", for documentation.
func GenerateFaviconSVG(projectName, accentColor string) string {
	initial := "D"
	if runes := []rune(projectName); len(runes) > 0 {
		initial = strings.ToUpper(string(runes[0]))
	}
	return `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 32 32">` +
		`<rect width="32" height="32" rx="4" fill="` + accentColor + `"/>` +
		`<text x="16" y="22" text-anchor="middle" fill="white" ` +
		`font-family="system-ui" font-size="18" font-weight="700">` +
		util.EscapeHTML(initial) + `</text>` +
		`</svg>`
}
