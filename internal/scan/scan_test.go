package scan_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/draw"
	"image/gif"
	"image/jpeg"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"nautilus/internal/scan"
	"nautilus/internal/testutil/require"
)

func picture(t *testing.T, format string, w, h int, c color.Color) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	draw.Draw(img, img.Bounds(), image.NewUniform(c), image.Point{}, draw.Src)
	var buf bytes.Buffer
	var err error
	switch format {
	case "jpeg":
		err = jpeg.Encode(&buf, img, nil)
	case "gif":
		err = gif.Encode(&buf, img, nil)
	default:
		err = png.Encode(&buf, img)
	}
	require.NoError(t, err)
	return buf.Bytes()
}

func TestValidate(t *testing.T) {
	t.Parallel()
	pngData := picture(t, "png", 30, 60, color.White)
	jpegData := picture(t, "jpeg", 60, 30, color.White)
	corrupt := bytes.Clone(pngData)
	corrupt[len(corrupt)-5] ^= 0xff
	oversized := bytes.Clone(pngData)
	binary.BigEndian.PutUint32(oversized[16:20], 5001)
	binary.BigEndian.PutUint32(oversized[20:24], 5000)
	binary.BigEndian.PutUint32(oversized[29:33], crc32.ChecksumIEEE(oversized[12:29]))
	tests := []struct {
		name        string
		data        []byte
		contentType string
		err         error
	}{
		{name: "png", data: pngData, contentType: "image/png"},
		{name: "jpeg", data: jpegData, contentType: "image/jpeg"},
		{name: "empty", err: scan.ErrInvalidImage},
		{name: "pdf", data: []byte("%PDF-1.7\n"), err: scan.ErrInvalidImage},
		{name: "text", data: []byte("scan"), err: scan.ErrInvalidImage},
		{name: "gif", data: picture(t, "gif", 2, 2, color.White), err: scan.ErrInvalidImage},
		{name: "tiff", data: []byte("II\x2a\x00\x08\x00\x00\x00"), err: scan.ErrInvalidImage},
		{name: "webp", data: []byte("RIFF\x18\x00\x00\x00WEBPVP8 "), err: scan.ErrInvalidImage},
		{name: "png missing end", data: pngData[:len(pngData)-12], err: scan.ErrInvalidImage},
		{name: "jpeg missing end", data: jpegData[:len(jpegData)-2], err: scan.ErrInvalidImage},
		{name: "corrupt png", data: corrupt, err: scan.ErrInvalidImage},
		{name: "pixel limit", data: oversized, err: scan.ErrTooLarge},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := scan.Validate(tt.data)
			require.ErrorIs(t, err, tt.err)
			require.Equal(t, tt.contentType, got)
		})
	}
}

func TestPDFLimits(t *testing.T) {
	t.Parallel()
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	tests := []struct {
		name  string
		ctx   context.Context
		pages [][]byte
		err   error
	}{
		{name: "no pages", ctx: t.Context(), err: scan.ErrInvalidImage},
		{name: "too many pages", ctx: t.Context(), pages: make([][]byte, scan.MaxPages+1), err: scan.ErrTooLarge},
		{name: "invalid page", ctx: t.Context(), pages: [][]byte{[]byte("invalid")}, err: scan.ErrInvalidImage},
		{name: "cancelled", ctx: cancelled, err: context.Canceled},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			data, err := scan.PDF(tt.ctx, tt.pages)
			require.ErrorIs(t, err, tt.err)
			require.Nil(t, data)
		})
	}
}

func TestPDF(t *testing.T) {
	t.Parallel()
	pages := [][]byte{
		picture(t, "jpeg", 300, 600, color.RGBA{R: 255, A: 255}),
		picture(t, "png", 600, 300, color.NRGBA{B: 255, A: 128}),
	}
	data, err := scan.PDF(t.Context(), pages)
	require.NoError(t, err)
	require.True(t, bytes.HasPrefix(data, []byte("%PDF-")))
	for _, tool := range []string{"pdfinfo", "pdftoppm", "pdfimages", "pdftotext"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skip("Poppler is needed to verify PDF rendering")
		}
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "scan.pdf")
	require.NoError(t, os.WriteFile(path, data, 0600))
	info, err := exec.CommandContext(t.Context(), "pdfinfo", "-f", "1", "-l", "2", path).CombinedOutput()
	require.NoError(t, err, string(info))
	require.Regexp(t, `Pages:\s+2`, string(info))
	require.Regexp(t, `Page\s+1 size:\s+72 x 144 pts`, string(info))
	require.Regexp(t, `Page\s+2 size:\s+144 x 72 pts`, string(info))
	images, err := exec.CommandContext(t.Context(), "pdfimages", "-list", path).CombinedOutput()
	require.NoError(t, err, string(images))
	require.Regexp(t, `1\s+0\s+image\s+300\s+600`, string(images))
	require.Regexp(t, `2\s+1\s+image\s+600\s+300`, string(images))
	text, err := exec.CommandContext(t.Context(), "pdftotext", path, "-").Output()
	require.NoError(t, err)
	require.Equal(t, "", strings.TrimSpace(string(text)))
	prefix := filepath.Join(dir, "page")
	result, err := exec.CommandContext(t.Context(), "pdftoppm", "-png", "-r", "300", path, prefix).CombinedOutput()
	require.NoError(t, err, string(result))
	for i, name := range []string{"page-1.png", "page-2.png"} {
		rendered, err := os.ReadFile(filepath.Join(dir, name))
		require.NoError(t, err)
		img, err := png.Decode(bytes.NewReader(rendered))
		require.NoError(t, err)
		wantSize := image.Pt(300, 600)
		wantColor := color.NRGBA{R: 254, A: 255}
		if i == 1 {
			wantSize = image.Pt(600, 300)
			wantColor = color.NRGBA{R: 127, G: 127, B: 255, A: 255}
		}
		require.Equal(t, wantSize, img.Bounds().Size())
		got := color.NRGBAModel.Convert(img.At(wantSize.X/2, wantSize.Y/2)).(color.NRGBA)
		require.InDelta(t, wantColor.R, got.R, 2)
		require.InDelta(t, wantColor.G, got.G, 2)
		require.InDelta(t, wantColor.B, got.B, 2)
		require.Equal(t, uint8(255), got.A)
	}
}

func TestPDFMaxPages(t *testing.T) {
	t.Parallel()
	page := picture(t, "png", 1, 1, color.White)
	pages := make([][]byte, scan.MaxPages)
	for i := range pages {
		pages[i] = page
	}
	data, err := scan.PDF(t.Context(), pages)
	require.NoError(t, err)
	require.NotEmpty(t, data)
}

func TestValidateByteLimit(t *testing.T) {
	t.Parallel()

	contentType, err := scan.Validate(make([]byte, scan.MaxPDFBytes+1))
	require.ErrorIs(t, err, scan.ErrTooLarge)
	require.Empty(t, contentType)
}

func TestPDFDeterministic(t *testing.T) {
	t.Parallel()

	pages := [][]byte{
		picture(t, "jpeg", 30, 60, color.White),
		picture(t, "png", 60, 30, color.NRGBA{R: 255, A: 128}),
	}
	first, err := scan.PDF(t.Context(), pages)
	require.NoError(t, err)
	require.Contains(t, string(first), "/CreationDate (D:19700101000000)")
	require.Contains(t, string(first), "/ModDate (D:19700101000000)")
	for range 5 {
		next, err := scan.PDF(t.Context(), pages)
		require.NoError(t, err)
		require.Equal(t, first, next)
	}
}

func TestPDFSourceByteLimit(t *testing.T) {
	t.Parallel()

	// Shared backing bytes keep the fixture small while the aggregate exceeds
	// the limit. The budget is checked before any image decoding.
	page := make([]byte, scan.MaxPDFBytes/scan.MaxPages+1)
	pages := make([][]byte, scan.MaxPages)
	for i := range pages {
		pages[i] = page
	}
	data, err := scan.PDF(t.Context(), pages)
	require.ErrorIs(t, err, scan.ErrTooLarge)
	require.Nil(t, data)
}
