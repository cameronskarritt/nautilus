package seed

import (
	"bytes"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"nautilus/internal/scan"
	"nautilus/internal/testutil/require"
)

const orgID = "12345678-1234-4234-8234-123456789abc"

func TestExecuteUpload(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	var wantNames []string
	var wantPages [][]byte
	for i := 1; i <= 10; i++ {
		ext := ".png"
		if i%2 == 0 {
			ext = ".jpeg"
		}
		name := fmt.Sprintf("invoice-page-%d%s", i, ext)
		wantPages = append(wantPages, writeImage(t, dir, name, i))
		if i == 1 {
			name = "invoice.png"
		}
		wantNames = append(wantNames, name)
	}
	// A corrupt standalone rendition must be ignored when numbered pages exist.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "invoice.jpg"), []byte("obsolete"), 0600))
	type request struct {
		method, path, cookie string
		fields, names        []string
		pages                [][]byte
		err                  error
	}
	requests := make(chan request, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := request{method: r.Method, path: r.URL.Path, cookie: r.Header.Get("Cookie")}
		reader, err := r.MultipartReader()
		if err == nil {
			for {
				part, nextErr := reader.NextPart()
				if nextErr == io.EOF {
					break
				}
				if nextErr != nil {
					err = nextErr
					break
				}
				data, readErr := io.ReadAll(part)
				if readErr != nil {
					err = readErr
					break
				}
				got.fields = append(got.fields, part.FormName())
				got.names = append(got.names, part.FileName())
				got.pages = append(got.pages, data)
			}
		}
		got.err = err
		requests <- got
		w.WriteHeader(http.StatusAccepted)
		fmt.Fprintf(w, `{"document":{"id":%q}}`, orgID)
	}))
	t.Cleanup(server.Close)
	var out bytes.Buffer
	err := execute(t.Context(), []string{"--dir", dir, "--url", server.URL + "/api/", "--org-id", orgID}, "admin-token", &out)
	require.NoError(t, err)
	got := <-requests
	require.NoError(t, got.err)
	require.Equal(t, http.MethodPost, got.method)
	require.Equal(t, "/api/admin/organizations/"+orgID+"/documents", got.path)
	require.Equal(t, "nautilus-session=admin-token", got.cookie)
	require.Equal(t, wantNames, got.names)
	require.Equal(t, wantPages, got.pages)
	for _, field := range got.fields {
		require.Equal(t, "file", field)
	}
	require.Contains(t, out.String(), "1/1\tinvoice.pdf\t"+orgID)
}

func TestExecuteDryRun(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeImage(t, dir, "zebra.png", 1)
	writeImage(t, dir, "alpha.jpg", 2)
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)
	var out bytes.Buffer
	err := execute(t.Context(), []string{"--dir", dir, "--url", server.URL, "--dry-run", "--limit", "1"}, "", &out)
	require.NoError(t, err)
	require.Zero(t, calls.Load())
	require.Equal(t, "alpha.pdf\t1 pages\nValidated 1 documents; no uploads made.\n", out.String())
}

func TestExecutePreflight(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name  string
		files []string
		bad   string
	}{
		{name: "missing first page", files: []string{"z-page-2.png"}},
		{name: "missing middle page", files: []string{"z-page-1.png", "z-page-3.png"}},
		{name: "duplicate page", files: []string{"z-page-1.png", "z-page-01.jpg"}},
		{name: "zero page", files: []string{"z-page-0.png"}},
		{name: "too many pages", files: []string{"z-page-101.png"}},
		{name: "corrupt later document", bad: "z.png"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			writeImage(t, dir, "a.png", 1)
			for _, name := range tt.files {
				writeImage(t, dir, name, 2)
			}
			if tt.bad != "" {
				require.NoError(t, os.WriteFile(filepath.Join(dir, tt.bad), []byte("not an image"), 0600))
			}
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.WriteHeader(http.StatusAccepted)
				fmt.Fprintf(w, `{"document":{"id":%q}}`, orgID)
			}))
			t.Cleanup(server.Close)
			err := execute(t.Context(), []string{"--dir", dir, "--url", server.URL, "--org-id", orgID}, "admin-token", io.Discard)
			require.Error(t, err)
			if tt.bad != "" {
				require.ErrorIs(t, err, scan.ErrInvalidImage)
			}
			require.Zero(t, calls.Load())
		})
	}
}

func TestExecuteStopsOnFailure(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name   string
		status int
		body   string
	}{
		{name: "server failure", status: http.StatusInternalServerError},
		{name: "malformed JSON", status: http.StatusAccepted, body: "{"},
		{name: "missing ID", status: http.StatusAccepted, body: `{"document":{}}`},
		{name: "invalid ID", status: http.StatusAccepted, body: `{"document":{"id":"invalid"}}`},
		{name: "redirect", status: http.StatusTemporaryRedirect},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			for _, name := range []string{"a.png", "b.png", "c.png"} {
				writeImage(t, dir, name, 1)
			}
			var forwarded, calls atomic.Int32
			destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				forwarded.Add(1)
				w.WriteHeader(http.StatusAccepted)
			}))
			t.Cleanup(destination.Close)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if calls.Add(1) == 1 {
					w.WriteHeader(http.StatusAccepted)
					fmt.Fprintf(w, `{"document":{"id":%q}}`, orgID)
					return
				}
				w.Header().Set("Location", destination.URL)
				w.WriteHeader(tt.status)
				fmt.Fprint(w, tt.body)
			}))
			t.Cleanup(server.Close)
			var out bytes.Buffer
			err := execute(t.Context(), []string{"--dir", dir, "--url", server.URL, "--org-id", orgID}, "admin-token", &out)
			require.Error(t, err)
			require.EqualValues(t, 2, calls.Load())
			require.Zero(t, forwarded.Load())
			require.Contains(t, out.String(), "Stopped after 1 accepted documents")
			require.NotContains(t, out.String(), "c.pdf")
			require.NotContains(t, out.String()+err.Error(), "admin-token")
		})
	}
}

func TestExecuteInvalidOptions(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name  string
		args  []string
		token string
	}{
		{name: "unknown flag", args: []string{"--unknown"}},
		{name: "negative limit", args: []string{"--limit", "-1"}},
		{name: "positional argument", args: []string{"extra"}},
		{name: "missing organization", token: "admin-token"},
		{name: "invalid organization", args: []string{"--org-id", "invalid"}, token: "admin-token"},
		{name: "missing session", args: []string{"--org-id", orgID}},
		{name: "invalid session", args: []string{"--org-id", orgID}, token: "bad\ntoken"},
		{name: "insecure remote URL", args: []string{"--dry-run", "--url", "http://example.com/api"}},
		{name: "URL credentials", args: []string{"--dry-run", "--url", "https://user:password@example.com/api"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			writeImage(t, dir, "invoice.png", 1)
			args := append([]string{"--dir", dir}, tt.args...)
			err := execute(t.Context(), args, tt.token, io.Discard)
			require.Error(t, err)
		})
	}
}

func writeImage(t *testing.T, dir, name string, width int) []byte {
	t.Helper()
	var buf bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, width, 1))
	if filepath.Ext(name) == ".png" {
		require.NoError(t, png.Encode(&buf, img))
	} else {
		require.NoError(t, jpeg.Encode(&buf, img, nil))
	}
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), buf.Bytes(), 0600))
	return buf.Bytes()
}
