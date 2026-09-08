package scan

import (
	"bytes"
	"context"
	"image"
	"image/draw"
	"image/jpeg"
	"image/png"
	"net/http"
	"strconv"
	"time"

	"github.com/go-pdf/fpdf"

	"nautilus/internal/errors"
)

const (
	MaxPages         = 100
	MaxPDFBytes      = 100 << 20
	maxPixels        = 25_000_000
	maxRetainedBytes = 256 << 20
)

var (
	ErrInvalidImage = errors.New("invalid scan image")
	ErrTooLarge     = errors.New("scan exceeds size limit")
)

func Validate(data []byte) (string, error) {
	_, contentType, err := decode(context.Background(), data)
	return contentType, err
}

func decode(ctx context.Context, data []byte) (image.Image, string, error) {
	if err := ctx.Err(); err != nil {
		return nil, "", errors.WithStack(err)
	}
	if len(data) > MaxPDFBytes {
		return nil, "", ErrTooLarge
	}
	contentType := http.DetectContentType(data)
	if contentType != "image/jpeg" && contentType != "image/png" {
		return nil, "", ErrInvalidImage
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, "", errors.Wrap(ErrInvalidImage, "read image dimensions")
	}
	if cfg.Width <= 0 || cfg.Height <= 0 {
		return nil, "", ErrInvalidImage
	}
	if int64(cfg.Width)*int64(cfg.Height) > maxPixels {
		return nil, "", ErrTooLarge
	}
	var img image.Image
	r := &reader{ctx: ctx, Reader: bytes.NewReader(data)}
	if contentType == "image/jpeg" {
		img, err = jpeg.Decode(r)
	} else {
		img, err = png.Decode(r)
	}
	if ctx.Err() != nil {
		return nil, "", errors.WithStack(ctx.Err())
	}
	if err != nil {
		return nil, "", errors.Wrap(ErrInvalidImage, "decode image")
	}
	return img, contentType, nil
}

// PDF embeds each image at its original resolution, on a page sized at 300 dpi.
// Encoded image buffers retained by the PDF library are capped at 256 MiB.
func PDF(ctx context.Context, pages [][]byte) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, errors.WithStack(err)
	}
	if len(pages) == 0 {
		return nil, ErrInvalidImage
	}
	if len(pages) > MaxPages {
		return nil, ErrTooLarge
	}
	total := 0
	for _, data := range pages {
		if len(data) > MaxPDFBytes-total {
			return nil, ErrTooLarge
		}
		total += len(data)
	}
	pdf := fpdf.New("P", "pt", "A4", "")
	pdf.SetCatalogSort(true)
	pdf.SetCreationDate(time.Unix(0, 0).UTC())
	pdf.SetModificationDate(time.Unix(0, 0).UTC())
	pdf.SetAutoPageBreak(false, 0)
	total = 0
	for i, data := range pages {
		img, contentType, err := decode(ctx, data)
		if err != nil {
			return nil, err
		}
		options := fpdf.ImageOptions{ImageType: "JPG"}
		if contentType == "image/png" {
			// Flatten transparency before embedding so every viewer sees white paper.
			opaque := image.NewRGBA(img.Bounds())
			draw.Draw(opaque, opaque.Bounds(), image.White, image.Point{}, draw.Src)
			draw.Draw(opaque, opaque.Bounds(), img, img.Bounds().Min, draw.Over)
			var buf output
			buf.ctx = ctx
			if err := png.Encode(&buf, opaque); err != nil {
				return nil, errors.Wrap(err, "encode scan page")
			}
			data = buf.Bytes()
			options.ImageType = "PNG"
		}
		// fpdf retains encoded data; allow slice growth and its PNG pixel/8
		// preallocation, so highly compressible pages cannot bypass the budget.
		retained := 2 * len(data)
		if contentType == "image/png" {
			retained = max(retained, img.Bounds().Dx()*img.Bounds().Dy()/8)
		}
		total += retained
		if total > maxRetainedBytes {
			return nil, ErrTooLarge
		}
		if err := ctx.Err(); err != nil {
			return nil, errors.WithStack(err)
		}
		name := strconv.Itoa(i)
		pdf.RegisterImageOptionsReader(name, options, bytes.NewReader(data))
		w, h := float64(img.Bounds().Dx())*72/300, float64(img.Bounds().Dy())*72/300
		pdf.AddPageFormat("P", fpdf.SizeType{Wd: w, Ht: h})
		pdf.ImageOptions(name, 0, 0, w, h, false, options, 0, "")
		if err := pdf.Error(); err != nil {
			return nil, errors.Wrap(err, "add scan page")
		}
	}
	var buf output
	buf.ctx = ctx
	if err := pdf.Output(&buf); err != nil {
		return nil, errors.Wrap(err, "encode scan PDF")
	}
	return buf.Bytes(), nil
}

type reader struct {
	ctx context.Context
	*bytes.Reader
}

func (r *reader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, errors.WithStack(err)
	}
	n, err := r.Reader.Read(p)
	return n, errors.WithStack(err)
}

type output struct {
	ctx context.Context
	bytes.Buffer
}

func (w *output) Write(p []byte) (int, error) {
	if err := w.ctx.Err(); err != nil {
		return 0, errors.WithStack(err)
	}
	if len(p) > MaxPDFBytes-w.Len() {
		return 0, ErrTooLarge
	}
	n, err := w.Buffer.Write(p)
	return n, errors.WithStack(err)
}
