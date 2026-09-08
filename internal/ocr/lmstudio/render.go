package lmstudio

import (
	"bytes"
	"context"
	"image"
	"image/color"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"io"
	"mime"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"

	"nautilus/internal/errors"
	"nautilus/internal/ocr"
)

const maxPageDimension = 1288

func render(ctx context.Context, data []byte, contentType string, visit func([]byte) error) error {
	if err := ctx.Err(); err != nil {
		return errors.Wrap(err, "OCR rendering canceled")
	}
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return errors.Wrap(ocr.ErrInvalidDocument, "invalid OCR content type")
	}
	if mediaType == "application/pdf" {
		return renderPDF(ctx, data, visit)
	}
	formats := map[string]string{"image/png": "png", "image/jpeg": "jpeg", "image/gif": "gif", "image/webp": "webp"}
	format, ok := formats[mediaType]
	if !ok {
		return errors.Wrap(ocr.ErrInvalidDocument, "unsupported OCR content type")
	}
	cfg, actual, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || actual != format || cfg.Width < 1 || cfg.Height < 1 || cfg.Width > 40_000_000/cfg.Height {
		return errors.Wrap(ocr.ErrInvalidDocument, "invalid OCR image or image exceeds pixel limit")
	}
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return errors.Wrap(ocr.ErrInvalidDocument, "decode OCR image")
	}
	if err := ctx.Err(); err != nil {
		return errors.Wrap(err, "OCR rendering canceled")
	}
	width, height := maxPageDimension, maxPageDimension
	if cfg.Width >= cfg.Height {
		height = max(1, cfg.Height*maxPageDimension/cfg.Width)
	} else {
		width = max(1, cfg.Width*maxPageDimension/cfg.Height)
	}
	dst := image.NewRGBA(image.Rect(0, 0, width, height))
	draw.Draw(dst, dst.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, src.Bounds(), draw.Over, nil)
	var page bytes.Buffer
	if err := png.Encode(&page, dst); err != nil {
		return errors.New("encode OCR image")
	}
	if err := ctx.Err(); err != nil {
		return errors.Wrap(err, "OCR rendering canceled")
	}
	if err := visit(page.Bytes()); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return errors.Wrap(err, "OCR rendering canceled")
	}
	return nil
}

func renderPDF(ctx context.Context, data []byte, visit func([]byte) error) error {
	info, err := poppler(ctx, data, 15*time.Second, 64<<10, "pdfinfo", "-")
	if err != nil {
		return err
	}
	pages, err := pageCount(info)
	if err != nil {
		return err
	}
	for page := 1; page <= pages; page++ {
		number := strconv.Itoa(page)
		png, err := poppler(ctx, data, 30*time.Second, 8<<20, "pdftoppm", "-f", number, "-l", number, "-singlefile", "-scale-to", strconv.Itoa(maxPageDimension), "-png", "-")
		if err != nil {
			return err
		}
		cfg, format, err := image.DecodeConfig(bytes.NewReader(png))
		if err != nil || format != "png" || cfg.Width < 1 || cfg.Height < 1 || cfg.Width > maxPageDimension || cfg.Height > maxPageDimension {
			return errors.Wrap(ocr.ErrInvalidDocument, "invalid rendered OCR page")
		}
		if err := visit(png); err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return errors.Wrap(err, "OCR rendering canceled")
		}
	}
	return nil
}

func pageCount(info []byte) (int, error) {
	count, found := 0, false
	for line := range strings.SplitSeq(string(info), "\n") {
		value, ok := strings.CutPrefix(line, "Pages:")
		if !ok {
			continue
		}
		n, err := strconv.Atoi(strings.TrimSpace(value))
		if found || err != nil || n < 1 || n > 50 {
			return 0, errors.Wrap(ocr.ErrInvalidDocument, "invalid PDF page count or page limit exceeded")
		}
		count, found = n, true
	}
	if !found {
		return 0, errors.Wrap(ocr.ErrInvalidDocument, "missing PDF page count")
	}
	return count, nil
}

func poppler(ctx context.Context, data []byte, timeout time.Duration, limit int, name string, args ...string) ([]byte, error) {
	child, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	output := limitedOutput{limit: limit, cancel: cancel}
	cmd := exec.CommandContext(child, name, args...)
	cmd.WaitDelay = time.Second
	cmd.Stdin = bytes.NewReader(data)
	cmd.Stdout = &output
	cmd.Stderr = io.Discard
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	err := cmd.Run()
	if ctx.Err() != nil {
		return nil, errors.Wrap(ctx.Err(), "OCR rendering canceled")
	}
	if output.exceeded {
		return nil, errors.Wrap(ocr.ErrInvalidDocument, "OCR rendering exceeds output limit")
	}
	if child.Err() != nil {
		return nil, errors.Wrap(child.Err(), "OCR rendering timed out")
	}
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) && exit.ExitCode() >= 0 {
			return nil, errors.Wrap(ocr.ErrInvalidDocument, "unable to render PDF")
		}
		return nil, errors.New("unable to run PDF renderer")
	}
	return output.buffer.Bytes(), nil
}

type limitedOutput struct {
	buffer   bytes.Buffer
	limit    int
	cancel   context.CancelFunc
	exceeded bool
}

func (w *limitedOutput) Write(p []byte) (int, error) {
	if len(p) > w.limit-w.buffer.Len() {
		w.exceeded = true
		w.cancel()
		return 0, errors.New("OCR rendering output limit exceeded")
	}
	n, _ := w.buffer.Write(p)
	return n, nil
}
