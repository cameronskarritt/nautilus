package lmstudio

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"nautilus/internal/errors"
	"nautilus/internal/ocr"
	"nautilus/internal/testutil/require"
)

func TestRenderImages(t *testing.T) {
	t.Parallel()
	for _, format := range []string{"png", "jpeg", "gif", "webp"} {
		t.Run(format, func(t *testing.T) {
			t.Parallel()
			var input bytes.Buffer
			src := image.NewRGBA(image.Rect(0, 0, 20, 10))
			src.Set(10, 5, color.RGBA{R: 255, A: 255})
			switch format {
			case "png":
				require.NoError(t, png.Encode(&input, src))
			case "jpeg":
				require.NoError(t, jpeg.Encode(&input, src, nil))
			case "gif":
				require.NoError(t, gif.Encode(&input, src, nil))
			case "webp":
				b, err := base64.StdEncoding.DecodeString("UklGRiIAAABXRUJQVlA4IBYAAAAwAQCdASoBAAEADsD+JaQAA3AAAAAA")
				require.NoError(t, err)
				input.Write(b)
			}
			calls := 0
			err := render(t.Context(), input.Bytes(), "image/"+format, func(page []byte) error {
				calls++
				decoded, err := png.Decode(bytes.NewReader(page))
				require.NoError(t, err)
				require.Equal(t, maxPageDimension, decoded.Bounds().Dx())
				wantHeight := maxPageDimension / 2
				if format == "webp" {
					wantHeight = maxPageDimension
				}
				require.Equal(t, wantHeight, decoded.Bounds().Dy())
				if format == "png" {
					require.Equal(t, color.RGBA{R: 255, G: 255, B: 255, A: 255}, color.RGBAModel.Convert(decoded.At(0, 0)))
				}
				return nil
			})
			require.NoError(t, err)
			require.Equal(t, 1, calls)
		})
	}
}

func TestRenderInvalidImages(t *testing.T) {
	t.Parallel()
	var valid bytes.Buffer
	require.NoError(t, png.Encode(&valid, image.NewRGBA(image.Rect(0, 0, 1, 1))))
	oversized := bytes.Clone(valid.Bytes())
	binary.BigEndian.PutUint32(oversized[16:20], 100_000_001)
	binary.BigEndian.PutUint32(oversized[29:33], crc32.ChecksumIEEE(oversized[12:29]))
	for _, tt := range []struct {
		name, contentType string
		data              []byte
	}{
		{"unsupported", "text/plain", []byte("secret")},
		{"invalid MIME", "???", valid.Bytes()},
		{"mismatched format", "image/jpeg", valid.Bytes()},
		{"corrupt PNG", "image/png", []byte("secret")},
		{"oversized decoded image", "image/png", oversized},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := render(t.Context(), tt.data, tt.contentType, func([]byte) error { t.Fatal("invalid image visited"); return nil })
			require.ErrorIs(t, err, ocr.ErrInvalidDocument)
			require.NotContains(t, err.Error(), "secret")
		})
	}
}

func TestRenderPDF(t *testing.T) {
	t.Parallel()
	requirePoppler(t)
	pages := []int{}
	err := render(t.Context(), testPDF(2), "application/pdf", func(page []byte) error {
		cfg, err := png.DecodeConfig(bytes.NewReader(page))
		require.NoError(t, err)
		require.Equal(t, maxPageDimension, cfg.Width)
		pages = append(pages, cfg.Height)
		return nil
	})
	require.NoError(t, err)
	require.Equal(t, []int{1288, 644}, pages)
}

func TestRenderInvalidPDF(t *testing.T) {
	t.Parallel()
	requirePoppler(t)
	for _, tt := range []struct {
		name string
		data []byte
	}{
		{"corrupt", []byte("private invalid PDF")},
		{"too many pages", testPDF(101)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := render(t.Context(), tt.data, "application/pdf", func([]byte) error { t.Fatal("invalid PDF visited"); return nil })
			require.ErrorIs(t, err, ocr.ErrInvalidDocument)
			require.NotContains(t, err.Error(), "private")
		})
	}
}

func TestRenderVisitorFailure(t *testing.T) {
	t.Parallel()
	requirePoppler(t)
	failure := errors.New("visitor failed")
	calls := 0
	err := render(t.Context(), testPDF(2), "application/pdf", func([]byte) error { calls++; return failure })
	require.ErrorIs(t, err, failure)
	require.Equal(t, 1, calls)
}

func TestRenderCancellation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	err := render(ctx, nil, "application/pdf", func([]byte) error { t.Fatal("canceled render visited"); return nil })
	require.ErrorIs(t, err, context.Canceled)
}

func TestPageCount(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name, info string
		want       int
	}{
		{"one", "Pages: 1\n", 1},
		{"previously rejected", "Pages: 51\n", 51},
		{"maximum", "Pages: 100\n", 100},
		{"other metadata", "Title: Synthetic\nPages: 2\nEncrypted: no", 2},
		{"zero", "Pages: 0", 0},
		{"above limit", "Pages: 101", 0},
		{"missing", "Title: Synthetic", 0},
		{"nonnumeric", "Pages: invalid", 0},
		{"duplicate metadata", "Title: Synthetic\nPages: 1\nPages: 2\n", 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := pageCount([]byte(tt.info))
			if tt.want == 0 {
				require.ErrorIs(t, err, ocr.ErrInvalidDocument)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestPopplerBounds(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"overflow", "timeout", "invalid", "missing", "signal"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			name, args := os.Args[0], []string{"-test.run=^TestRendererProcess$", "--", mode}
			timeout := 5 * time.Second
			if mode == "timeout" {
				timeout = 50 * time.Millisecond
			}
			if mode == "missing" {
				name = "nautilus-nonexistent-pdf-renderer"
				args = nil
			}
			_, err := poppler(t.Context(), nil, timeout, 1024, name, args...)
			require.Error(t, err)
			require.NotContains(t, err.Error(), "private")
			switch mode {
			case "overflow", "invalid":
				require.ErrorIs(t, err, ocr.ErrInvalidDocument)
			case "timeout":
				require.ErrorIs(t, err, context.DeadlineExceeded)
			case "missing", "signal":
				require.NotErrorIs(t, err, ocr.ErrInvalidDocument)
			}
		})
	}
}

func TestRendererProcess(t *testing.T) {
	if len(os.Args) < 2 || os.Args[len(os.Args)-2] != "--" {
		return
	}
	switch os.Args[len(os.Args)-1] {
	case "overflow":
		fmt.Print(strings.Repeat("x", 2048))
	case "timeout":
		time.Sleep(time.Minute)
	case "signal":
		process, err := os.FindProcess(os.Getpid())
		if err != nil {
			os.Exit(2)
		}
		_ = process.Kill()
	case "invalid":
		fmt.Fprint(os.Stderr, "private document metadata")
		os.Exit(1)
	}
	os.Exit(0)
}

func requirePoppler(t *testing.T) {
	t.Helper()
	for _, name := range []string{"pdfinfo", "pdftoppm"} {
		if _, err := exec.LookPath(name); err != nil {
			t.Skip("Poppler is not installed")
		}
	}
}

func testPDF(pages int) []byte {
	objects := []string{"<< /Type /Catalog /Pages 2 0 R >>", ""}
	var kids strings.Builder
	for i := range pages {
		fmt.Fprintf(&kids, "%d 0 R ", i+3)
		objects = append(objects, fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 %d 100] /Resources << >> >>", 100*(i+1)))
	}
	objects[1] = fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", kids.String(), pages)
	var pdf bytes.Buffer
	pdf.WriteString("%PDF-1.4\n")
	offsets := []int{0}
	for i, object := range objects {
		offsets = append(offsets, pdf.Len())
		fmt.Fprintf(&pdf, "%d 0 obj\n%s\nendobj\n", i+1, object)
	}
	xref := pdf.Len()
	fmt.Fprintf(&pdf, "xref\n0 %d\n0000000000 65535 f \n", len(offsets))
	for _, offset := range offsets[1:] {
		fmt.Fprintf(&pdf, "%010d 00000 n \n", offset)
	}
	fmt.Fprintf(&pdf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(offsets), xref)
	return pdf.Bytes()
}
