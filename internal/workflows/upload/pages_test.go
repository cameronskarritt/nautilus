package upload_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"image"
	"image/color"
	"image/png"
	"io"
	"strconv"
	"testing"

	"go.temporal.io/sdk/temporal"

	"nautilus/internal/crypto/encrypt"
	"nautilus/internal/database/documents"
	"nautilus/internal/database/organizations"
	"nautilus/internal/errors"
	"nautilus/internal/objectstore"
	"nautilus/internal/optional"
	"nautilus/internal/testutil"
	"nautilus/internal/testutil/require"
	"nautilus/internal/workflows/upload"
)

func TestSourcePages(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"success", "wrong binding", "missing page", "size mismatch", "store retry", "OCR failure"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			db := testutil.SetupTestDB(t)
			org, err := organizations.Create(t.Context(), db, t.Name(), "scan", false, optional.Empty[organizations.Settings]())
			require.NoError(t, err)
			var images [][]byte
			var opts []documents.PageOptions
			for _, c := range []color.Color{color.Black, color.White} {
				img := image.NewRGBA(image.Rect(0, 0, 4, 5))
				img.Set(0, 0, c)
				var buf bytes.Buffer
				require.NoError(t, png.Encode(&buf, img))
				images = append(images, buf.Bytes())
				opts = append(opts, documents.PageOptions{ContentType: "image/png", Size: int64(buf.Len())})
			}
			if name == "size mismatch" {
				opts[0].Size++
			}
			doc, err := documents.Create(t.Context(), db, org.ID, &documents.CreateOptions{Filename: "scan.pdf", ContentType: "application/pdf", Pages: opts})
			require.NoError(t, err)
			pages, err := documents.ListPages(t.Context(), db, org.ID, doc.ExternalID)
			require.NoError(t, err)
			enc := encrypt.ForOrganization(ocrKeys{}, org.ExternalID)
			store := &pageStore{objects: make(map[string][]byte)}
			for i, page := range pages {
				record := doc.ExternalID + "/" + strconv.Itoa(page.Number)
				if name == "wrong binding" {
					record = doc.ExternalID + "/" + strconv.Itoa(2-i)
				}
				store.objects[page.ObjectKey], err = enc.Seal(t.Context(), images[i], encrypt.Binding{Purpose: "document-page", RecordID: record})
				require.NoError(t, err)
			}
			if name == "missing page" {
				delete(store.objects, pages[1].ObjectKey)
			}
			extractor := &pageOCR{t: t, images: images}
			a := upload.Activities{DB: db, Keys: ocrKeys{}, Store: store, OCR: extractor}
			input := upload.Input{OrganizationID: org.ID, DocumentID: doc.ExternalID}
			if name == "store retry" {
				store.fail = true
			}
			err = a.Finalize(t.Context(), input)
			if name == "wrong binding" || name == "missing page" || name == "size mismatch" || name == "store retry" {
				require.Error(t, err)
				require.NotContains(t, err.Error(), "sensitive")
				got, e := documents.GetByExternalID(t.Context(), db, org.ID, doc.ExternalID)
				require.NoError(t, e)
				if name == "size mismatch" {
					require.Equal(t, "failed", got.Status.String())
				} else {
					require.Equal(t, "uploading", got.Status.String())
				}
				require.Zero(t, got.Size)
				require.Nil(t, store.objects[got.PDFKey])
				if name != "store retry" {
					return
				}
				store.fail = false
				require.NoError(t, a.Finalize(t.Context(), input))
			} else {
				require.NoError(t, err)
			}
			got, err := documents.GetByExternalID(t.Context(), db, org.ID, doc.ExternalID)
			require.NoError(t, err)
			require.Equal(t, "uploaded", got.Status.String())
			pdf, err := enc.Open(t.Context(), store.objects[got.PDFKey], encrypt.Binding{Purpose: "document", RecordID: doc.ExternalID})
			require.NoError(t, err)
			require.True(t, bytes.HasPrefix(pdf, []byte("%PDF-")))
			require.Equal(t, int64(len(pdf)), got.Size)
			require.Contains(t, string(pdf), "/Count 2")
			digest := sha256.Sum256(pdf)
			require.Equal(t, doc.ObjectKey+"/pdf/"+hex.EncodeToString(digest[:]), got.PDFKey)
			require.Equal(t, hex.EncodeToString(digest[:]), got.SHA256)
			require.Nil(t, store.objects[doc.ObjectKey])
			canonical := bytes.Clone(store.objects[got.PDFKey])
			require.NoError(t, a.Finalize(t.Context(), input))
			require.Equal(t, canonical, store.objects[got.PDFKey])
			store.gets = nil
			if name == "OCR failure" {
				extractor.fail = true
			}
			err = a.Extract(t.Context(), input)
			if name == "OCR failure" {
				var appErr *temporal.ApplicationError
				require.ErrorAs(t, err, &appErr)
				require.False(t, appErr.NonRetryable())
				require.Nil(t, appErr.Unwrap())
				require.NotContains(t, err.Error(), "sensitive")
				require.Nil(t, store.objects[doc.ObjectKey+"/ocr"])
				extractor.fail = false
				extractor.calls = 0
				store.gets = nil
				require.NoError(t, a.Extract(t.Context(), input))
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, []string{pages[0].ObjectKey, pages[1].ObjectKey}, store.gets)
			text, err := enc.Open(t.Context(), store.objects[doc.ObjectKey+"/ocr"], encrypt.Binding{Purpose: "document-ocr", RecordID: doc.ExternalID})
			require.NoError(t, err)
			require.Equal(t, "page 1\npage 2", string(text))
			for _, page := range pages {
				require.NotEmpty(t, store.objects[page.ObjectKey])
			}
			require.Equal(t, canonical, store.objects[got.PDFKey])
		})
	}
}

type pageStore struct {
	objectstore.Store
	objects   map[string][]byte
	gets      []string
	fail      bool
	beforePut func(string, []byte)
}

func (s *pageStore) Get(_ context.Context, key string, _ *objectstore.GetOptions) (*objectstore.Object, error) {
	s.gets = append(s.gets, key)
	data, ok := s.objects[key]
	if !ok {
		return nil, errors.New("sensitive missing object")
	}
	return &objectstore.Object{Body: io.NopCloser(bytes.NewReader(data))}, nil
}
func (s *pageStore) Put(_ context.Context, key string, data io.Reader, opts *objectstore.PutOptions) error {
	if s.fail {
		return errors.New("sensitive storage failure")
	}
	if opts.ContentType.Data != "application/octet-stream" {
		return errors.New("unexpected object content type")
	}
	b, err := io.ReadAll(data)
	if err != nil {
		return errors.WithStack(err)
	}
	if s.beforePut != nil {
		s.beforePut(key, b)
	}
	s.objects[key] = b
	return nil
}

type pageOCR struct {
	t      *testing.T
	images [][]byte
	calls  int
	fail   bool
}

func (o *pageOCR) Extract(_ context.Context, r io.Reader, contentType string) (string, error) {
	require.Equal(o.t, "image/png", contentType)
	b, err := io.ReadAll(r)
	require.NoError(o.t, err)
	require.Equal(o.t, o.images[o.calls], b)
	o.calls++
	if o.fail {
		return "", errors.New("sensitive provider error")
	}
	return "page " + strconv.Itoa(o.calls), nil
}

func TestFinalizeConcurrentPublication(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	org, err := organizations.Create(t.Context(), db, t.Name(), "publication", false, optional.Empty[organizations.Settings]())
	require.NoError(t, err)
	var source bytes.Buffer
	require.NoError(t, png.Encode(&source, image.NewRGBA(image.Rect(0, 0, 4, 5))))
	doc, err := documents.Create(t.Context(), db, org.ID, &documents.CreateOptions{Filename: "scan.pdf", ContentType: "application/pdf", Pages: []documents.PageOptions{{ContentType: "image/png", Size: int64(source.Len())}}})
	require.NoError(t, err)
	pages, err := documents.ListPages(t.Context(), db, org.ID, doc.ExternalID)
	require.NoError(t, err)
	enc := encrypt.ForOrganization(ocrKeys{}, org.ExternalID)
	ciphertext, err := enc.Seal(t.Context(), source.Bytes(), encrypt.Binding{Purpose: "document-page", RecordID: doc.ExternalID + "/1"})
	require.NoError(t, err)
	store := &pageStore{objects: map[string][]byte{pages[0].ObjectKey: ciphertext}}
	a := upload.Activities{DB: db, Keys: ocrKeys{}, Store: store}
	input := upload.Input{OrganizationID: org.ID, DocumentID: doc.ExternalID}
	var winnerKey, staleKey string
	var winnerPDF []byte
	store.beforePut = func(key string, encrypted []byte) {
		store.beforePut = nil
		staleKey = key
		pdf, err := enc.Open(t.Context(), encrypted, encrypt.Binding{Purpose: "document", RecordID: doc.ExternalID})
		require.NoError(t, err)
		// A competing worker version finishes a different valid PDF while this
		// attempt is still storing its object. Publication must preserve its winner.
		winnerPDF = append(pdf, []byte("\n% competing renderer\n")...)
		digest := sha256.Sum256(winnerPDF)
		winnerKey = doc.ObjectKey + "/pdf/" + hex.EncodeToString(digest[:])
		stored, err := enc.Seal(t.Context(), winnerPDF, encrypt.Binding{Purpose: "document", RecordID: doc.ExternalID})
		require.NoError(t, err)
		require.NoError(t, store.Put(t.Context(), winnerKey, bytes.NewReader(stored), &objectstore.PutOptions{ContentType: optional.Set("application/octet-stream")}))
		published, err := documents.PublishPDF(t.Context(), db, org.ID, doc.ExternalID, winnerKey, int64(len(winnerPDF)))
		require.NoError(t, err)
		require.NotNil(t, published)
	}
	require.NoError(t, a.Finalize(t.Context(), input))
	require.NotEqual(t, winnerKey, staleKey)
	require.NotEmpty(t, store.objects[staleKey])
	got, err := documents.GetByExternalID(t.Context(), db, org.ID, doc.ExternalID)
	require.NoError(t, err)
	require.Equal(t, "uploaded", got.Status.String())
	require.Equal(t, winnerKey, got.PDFKey)
	require.Equal(t, int64(len(winnerPDF)), got.Size)
	pdf, err := enc.Open(t.Context(), store.objects[got.PDFKey], encrypt.Binding{Purpose: "document", RecordID: doc.ExternalID})
	require.NoError(t, err)
	require.Equal(t, winnerPDF, pdf)
	require.NoError(t, a.Finalize(t.Context(), input))
	got, err = documents.GetByExternalID(t.Context(), db, org.ID, doc.ExternalID)
	require.NoError(t, err)
	require.Equal(t, winnerKey, got.PDFKey)
	require.Equal(t, int64(len(winnerPDF)), got.Size)
}
