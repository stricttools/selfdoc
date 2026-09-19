package build

import (
	"encoding/binary"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/stricttools/selfdoc/internal/util"
)

// imageDimensions is a decoded image's pixel size.
type imageDimensions struct {
	Width  int
	Height int
}

// readPNGDimensions reads width and height from a PNG file's IHDR chunk.
//
// The IHDR chunk starts at byte 16. Width is 4 bytes big-endian at offset 16,
// height is 4 bytes big-endian at offset 20. It reports false when the file is
// too short or is not a PNG.
func readPNGDimensions(filepathArg string) (imageDimensions, bool) {
	header, ok := readHeader(filepathArg, 24)
	if !ok || len(header) < 24 {
		return imageDimensions{}, false
	}
	if string(header[:8]) != "\x89PNG\r\n\x1a\n" {
		return imageDimensions{}, false
	}
	return imageDimensions{
		Width:  int(binary.BigEndian.Uint32(header[16:20])),
		Height: int(binary.BigEndian.Uint32(header[20:24])),
	}, true
}

// readGIFDimensions reads width and height from a GIF file header.
//
// Bytes 6-7 are the width and bytes 8-9 the height, both little-endian
// uint16. It reports false when the file is too short or is not a GIF.
func readGIFDimensions(filepathArg string) (imageDimensions, bool) {
	header, ok := readHeader(filepathArg, 10)
	if !ok || len(header) < 10 {
		return imageDimensions{}, false
	}
	if string(header[:3]) != "GIF" {
		return imageDimensions{}, false
	}
	return imageDimensions{
		Width:  int(binary.LittleEndian.Uint16(header[6:8])),
		Height: int(binary.LittleEndian.Uint16(header[8:10])),
	}, true
}

// readJPEGDimensions reads width and height from a JPEG file by walking its
// SOF markers.
//
// It walks the segments looking for SOF0 through SOF3 (0xFFC0 through
// 0xFFC3); at the first one it reads the height and then the width, each two
// bytes big-endian, after the segment length and the precision byte. It
// reports false for a file that is not a JPEG, is truncated, or cannot be
// read.
func readJPEGDimensions(filepathArg string) (imageDimensions, bool) {
	file, err := os.Open(filepathArg)
	if err != nil {
		return imageDimensions{}, false
	}
	defer file.Close()

	soi := make([]byte, 2)
	if _, err := io.ReadFull(file, soi); err != nil {
		return imageDimensions{}, false
	}
	if soi[0] != 0xFF || soi[1] != 0xD8 {
		return imageDimensions{}, false
	}
	for {
		marker := make([]byte, 2)
		if _, err := io.ReadFull(file, marker); err != nil {
			return imageDimensions{}, false
		}
		if marker[0] != 0xFF {
			return imageDimensions{}, false
		}
		if marker[1] >= 0xC0 && marker[1] <= 0xC3 {
			seg := make([]byte, 7)
			if _, err := io.ReadFull(file, seg); err != nil {
				return imageDimensions{}, false
			}
			return imageDimensions{
				Height: int(binary.BigEndian.Uint16(seg[3:5])),
				Width:  int(binary.BigEndian.Uint16(seg[5:7])),
			}, true
		}
		lengthBytes := make([]byte, 2)
		if _, err := io.ReadFull(file, lengthBytes); err != nil {
			return imageDimensions{}, false
		}
		// The declared length counts the two length bytes themselves.
		segLen := int(binary.BigEndian.Uint16(lengthBytes))
		if _, err := file.Seek(int64(segLen-2), io.SeekCurrent); err != nil {
			return imageDimensions{}, false
		}
	}
}

// readWebPDimensions reads width and height from a WebP file, in the VP8
// (lossy), VP8L (lossless) and VP8X (extended) sub-formats. It reports false
// for anything else, and for a file that is truncated or cannot be read.
func readWebPDimensions(filepathArg string) (imageDimensions, bool) {
	data, ok := readHeader(filepathArg, 30)
	if !ok || len(data) < 16 {
		return imageDimensions{}, false
	}
	if string(data[0:4]) != "RIFF" || string(data[8:12]) != "WEBP" {
		return imageDimensions{}, false
	}
	switch string(data[12:16]) {
	case "VP8 ":
		if len(data) < 30 {
			return imageDimensions{}, false
		}
		return imageDimensions{
			Width:  int(binary.LittleEndian.Uint16(data[26:28]) & 0x3FFF),
			Height: int(binary.LittleEndian.Uint16(data[28:30]) & 0x3FFF),
		}, true
	case "VP8L":
		if len(data) < 25 {
			return imageDimensions{}, false
		}
		bits := binary.LittleEndian.Uint32(data[21:25])
		return imageDimensions{
			Width:  int(bits&0x3FFF) + 1,
			Height: int((bits>>14)&0x3FFF) + 1,
		}, true
	case "VP8X":
		if len(data) < 30 {
			return imageDimensions{}, false
		}
		return imageDimensions{
			Width:  uint24LE(data[24:27]) + 1,
			Height: uint24LE(data[27:30]) + 1,
		}, true
	}
	return imageDimensions{}, false
}

// uint24LE decodes a three-byte little-endian unsigned integer.
func uint24LE(b []byte) int {
	return int(b[0]) | int(b[1])<<8 | int(b[2])<<16
}

// readHeader reads up to n bytes from the head of a file, reporting false when
// the file cannot be opened or read at all. A short file yields the bytes it
// has, as Python's read(n) does.
func readHeader(path string, n int) ([]byte, bool) {
	file, err := os.Open(path)
	if err != nil {
		return nil, false
	}
	defer file.Close()
	buf := make([]byte, n)
	read, err := io.ReadFull(file, buf)
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		return nil, false
	}
	return buf[:read], true
}

// getImageDimensions reads an image file's pixel size, choosing the reader by
// the file's extension. It reports false for an unsupported format and for a
// file it cannot read.
func getImageDimensions(filepathArg string) (imageDimensions, bool) {
	switch strings.ToLower(filepath.Ext(filepathArg)) {
	case ".png":
		return readPNGDimensions(filepathArg)
	case ".gif":
		return readGIFDimensions(filepathArg)
	case ".jpg", ".jpeg":
		return readJPEGDimensions(filepathArg)
	case ".webp":
		return readWebPDimensions(filepathArg)
	}
	return imageDimensions{}, false
}

// imgTagPattern matches an img element carrying a double-quoted src.
var imgTagPattern = regexp.MustCompile(`<img` + util.PythonSpaceClass + `[^>]*src="([^"]+)"[^>]*>`)

// AddImageDimensions adds width and height attributes to every img element
// whose source file exists on disk.
//
// Each src is resolved relative to the page's own directory inside docsDir;
// an external URL is left alone, and so is a file whose format none of the
// readers recognizes. The attributes are inserted before the tag's first ">",
// which is where the Python inserted them and therefore what every rendered
// page states.
func AddImageDimensions(htmlText, docsDir, pageRelPath string) string {
	pageDir := filepath.Dir(filepath.Join(docsDir, pageRelPath))
	return replaceAllGroups(imgTagPattern, htmlText, func(fullTag string, groups []string) string {
		src := groups[0]
		if strings.HasPrefix(src, "http://") || strings.HasPrefix(src, "https://") ||
			strings.HasPrefix(src, "//") {
			return fullTag
		}
		imgPath := filepath.Clean(filepath.Join(pageDir, src))
		dims, ok := getImageDimensions(imgPath)
		if !ok {
			return fullTag
		}
		return strings.Replace(fullTag, ">",
			` width="`+strconv.Itoa(dims.Width)+`" height="`+strconv.Itoa(dims.Height)+`">`, 1)
	})
}

// replaceAllGroups rewrites every match of re in s through repl, which is
// handed the whole match and its capture groups -- the shape of Python's
// re.sub with a function replacement.
func replaceAllGroups(re *regexp.Regexp, s string, repl func(whole string, groups []string) string) string {
	matches := re.FindAllStringSubmatchIndex(s, -1)
	if matches == nil {
		return s
	}
	var out strings.Builder
	last := 0
	for _, m := range matches {
		out.WriteString(s[last:m[0]])
		groups := make([]string, 0, len(m)/2-1)
		for g := 1; g < len(m)/2; g++ {
			if m[2*g] < 0 {
				groups = append(groups, "")
				continue
			}
			groups = append(groups, s[m[2*g]:m[2*g+1]])
		}
		out.WriteString(repl(s[m[0]:m[1]], groups))
		last = m[1]
	}
	out.WriteString(s[last:])
	return out.String()
}
