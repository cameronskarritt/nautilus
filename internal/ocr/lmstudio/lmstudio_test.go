package lmstudio_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"

	"nautilus/internal/errors"
	"nautilus/internal/ocr"
	"nautilus/internal/ocr/lmstudio"
	"nautilus/internal/testutil/require"
)

const frontmatter = "---\nprimary_language: en\nis_rotation_valid: true\nrotation_correction: 0\nis_table: false\nis_diagram: false\n---\n"

func TestNew(t *testing.T) {
	t.Parallel()
	for _, cfg := range []lmstudio.Config{
		{Model: "test"},
		{URL: "http://localhost/v1"},
		{URL: "file:///secret", Model: "test"},
		{URL: "http://secret:secret@localhost/v1", Model: "test"},
		{URL: "http://localhost/v1?key=secret", Model: "test"},
		{URL: "http://localhost/v1#secret", Model: "test"},
		{URL: "http://localhost/v1", Model: "\xff"},
		{URL: "http://localhost/v1", Model: strings.Repeat("x", 513)},
	} {
		_, err := lmstudio.New(cfg)
		require.Error(t, err)
		require.NotContains(t, err.Error(), "secret")
	}
}

func TestExtract(t *testing.T) {
	t.Parallel()
	type request struct {
		method, path, auth, content string
		body                        []byte
	}
	requests := make(chan request, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		requests <- request{r.Method, r.URL.Path, r.Header.Get("Authorization"), r.Header.Get("Content-Type"), body}
		_, _ = io.WriteString(w, response(t, frontmatter+"Invoice 42\nTotal: $12.00", "stop"))
	}))
	t.Cleanup(server.Close)
	client, err := lmstudio.New(lmstudio.Config{URL: server.URL + "/proxy/v1/", Model: "test", APIKey: "secret"})
	require.NoError(t, err)
	text, err := client.Extract(t.Context(), bytes.NewReader(testPNG(t)), "image/png")
	require.NoError(t, err)
	require.Equal(t, "Invoice 42\nTotal: $12.00", text)
	req := <-requests
	require.Equal(t, "POST", req.method)
	require.Equal(t, "/proxy/v1/chat/completions", req.path)
	require.Equal(t, "Bearer secret", req.auth)
	require.Equal(t, "application/json", req.content)
	var body struct {
		Model       string `json:"model"`
		Temperature int    `json:"temperature"`
		MaxTokens   int    `json:"max_tokens"`
		Stream      bool   `json:"stream"`
		Messages    []struct {
			Role    string `json:"role"`
			Content []struct {
				Type     string `json:"type"`
				Text     string `json:"text"`
				ImageURL struct {
					URL string `json:"url"`
				} `json:"image_url"`
			} `json:"content"`
		} `json:"messages"`
	}
	require.NoError(t, json.Unmarshal(req.body, &body))
	require.Equal(t, "test", body.Model)
	require.Zero(t, body.Temperature)
	require.Equal(t, 4096, body.MaxTokens)
	require.False(t, body.Stream)
	require.Len(t, body.Messages, 1)
	require.Equal(t, "user", body.Messages[0].Role)
	require.Len(t, body.Messages[0].Content, 2)
	require.Equal(t, "text", body.Messages[0].Content[0].Type)
	require.Contains(t, body.Messages[0].Content[0].Text, "Convert equations to LateX and tables to HTML.")
	require.Contains(t, body.Messages[0].Content[0].Text, "primary_language, is_rotation_valid, rotation_correction, is_table, and is_diagram")
	part := body.Messages[0].Content[1]
	require.Equal(t, "image_url", part.Type)
	require.True(t, strings.HasPrefix(part.ImageURL.URL, "data:image/png;base64,"))
	encoded, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(part.ImageURL.URL, "data:image/png;base64,"))
	require.NoError(t, err)
	_, err = png.Decode(bytes.NewReader(encoded))
	require.NoError(t, err)
}

func TestExtractResponses(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name, body, want string
		status           int
		wantErr          bool
	}{
		{name: "YAML boolean casing", body: response(t, strings.ReplaceAll(strings.ReplaceAll(frontmatter, "true", "True"), "false", "False")+"Text", "stop"), want: "Text"},
		{name: "blank metadata line", body: response(t, strings.ReplaceAll(frontmatter, "is_table", "\nis_table")+"Text", "stop"), want: "Text"},
		{name: "blank page", body: response(t, strings.Replace(frontmatter, "en", "null", 1), "stop")},
		{name: "quoted language", body: response(t, strings.Replace(frontmatter, "en", `"en"`, 1)+"Text", "stop"), want: "Text"},
		{name: "CRLF", body: response(t, strings.ReplaceAll(frontmatter, "\n", "\r\n")+"Text", "stop"), want: "Text"},
		{name: "HTTP error", status: 500, body: "secret provider text", wantErr: true},
		{name: "redirect", status: 307, body: "secret provider text", wantErr: true},
		{name: "malformed JSON", body: "secret provider text", wantErr: true},
		{name: "invalid UTF8", body: "\xff", wantErr: true},
		{name: "missing choices", body: `{}`, wantErr: true},
		{name: "null content", body: `{"choices":[{"finish_reason":"stop","message":{"content":null}}]}`, wantErr: true},
		{name: "array content", body: `{"choices":[{"finish_reason":"stop","message":{"content":[]}}]}`, wantErr: true},
		{name: "truncated", body: response(t, frontmatter+"partial secret", "length"), wantErr: true},
		{name: "missing finish reason", body: response(t, frontmatter, ""), wantErr: true},
		{name: "missing frontmatter", body: response(t, "secret text", "stop"), wantErr: true},
		{name: "missing boundary", body: response(t, strings.TrimSuffix(frontmatter, "---\n"), "stop"), wantErr: true},
		{name: "invalid boundary", body: response(t, frontmatter[:len(frontmatter)-1]+"secret", "stop"), wantErr: true},
		{name: "missing field", body: response(t, strings.Replace(frontmatter, "is_table: false\n", "", 1), "stop"), wantErr: true},
		{name: "duplicate field", body: response(t, strings.Replace(frontmatter, "is_table: false", "is_table: false\nis_table: true", 1), "stop"), wantErr: true},
		{name: "unknown field", body: response(t, strings.Replace(frontmatter, "is_table:", "secret:", 1), "stop"), wantErr: true},
		{name: "nonboolean", body: response(t, strings.Replace(frontmatter, "is_table: false", "is_table: secret", 1), "stop"), wantErr: true},
		{name: "structured language", body: response(t, strings.Replace(frontmatter, "primary_language: en", "primary_language: [en]", 1), "stop"), wantErr: true},
		{name: "rotation required", body: response(t, strings.Replace(frontmatter, "rotation_correction: 0", "rotation_correction: 90", 1), "stop"), wantErr: true},
		{name: "invalid orientation", body: response(t, strings.Replace(frontmatter, "is_rotation_valid: true", "is_rotation_valid: false", 1), "stop"), wantErr: true},
		{name: "invalid rotation", body: response(t, strings.Replace(frontmatter, "rotation_correction: 0", "rotation_correction: 45", 1), "stop"), wantErr: true},
		{name: "model mismatch", body: strings.Replace(response(t, frontmatter, "stop"), `"test"`, `"secret"`, 1), wantErr: true},
		{name: "oversized", body: strings.Repeat(" ", (1<<20)+1), wantErr: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if tt.status != 0 {
					w.Header().Set("Location", "/secret")
					w.WriteHeader(tt.status)
				}
				_, _ = io.WriteString(w, tt.body)
			}))
			t.Cleanup(server.Close)
			client, err := lmstudio.New(lmstudio.Config{URL: server.URL, Model: "test"})
			require.NoError(t, err)
			text, err := client.Extract(t.Context(), bytes.NewReader(testPNG(t)), "image/png")
			if tt.wantErr {
				require.Error(t, err)
				require.Empty(t, text)
				require.NotContains(t, err.Error(), "secret")
				require.NotContains(t, err.Error(), server.URL)
				require.Equal(t, tt.name == "rotation required" || tt.name == "invalid orientation" || tt.name == "truncated", errors.Is(err, ocr.ErrInvalidDocument))
			} else {
				require.NoError(t, err)
				require.Equal(t, tt.want, text)
			}
			require.EqualValues(t, 1, calls.Load())
		})
	}
}

func TestExtractInputErrors(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name    string
		reader  io.Reader
		invalid bool
	}{
		{name: "missing", invalid: true},
		{name: "oversized", reader: strings.NewReader(strings.Repeat("x", (16<<20)+1)), invalid: true},
		{name: "read failure", reader: failedReader{}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			client, err := lmstudio.New(lmstudio.Config{URL: "http://localhost:1", Model: "test"})
			require.NoError(t, err)
			text, err := client.Extract(t.Context(), tt.reader, "image/png")
			require.Error(t, err)
			require.Empty(t, text)
			require.Equal(t, tt.invalid, errors.Is(err, ocr.ErrInvalidDocument))
			require.NotContains(t, err.Error(), "secret")
		})
	}
}

func TestExtractCancellation(t *testing.T) {
	t.Parallel()
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		close(started)
		<-r.Context().Done()
	}))
	t.Cleanup(server.Close)
	client, err := lmstudio.New(lmstudio.Config{URL: server.URL, Model: "test"})
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	result := make(chan error, 1)
	data := testPNG(t)
	go func() { _, err := client.Extract(ctx, bytes.NewReader(data), "image/png"); result <- err }()
	<-started
	cancel()
	require.ErrorIs(t, <-result, context.Canceled)
	_, err = client.Extract(ctx, nil, "image/png")
	require.ErrorIs(t, err, context.Canceled)
}

func TestExtractNetworkError(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	server.Close()
	client, err := lmstudio.New(lmstudio.Config{URL: server.URL + "/secret", Model: "test", APIKey: "secret"})
	require.NoError(t, err)
	_, err = client.Extract(t.Context(), bytes.NewReader(testPNG(t)), "image/png")
	require.Error(t, err)
	require.NotContains(t, err.Error(), "secret")
	require.NotContains(t, err.Error(), server.URL)
}

func testPNG(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, image.NewGray(image.Rect(0, 0, 32, 32))))
	return buf.Bytes()
}

func response(t *testing.T, content, finish string) string {
	t.Helper()
	body, err := json.Marshal(map[string]any{"model": "test", "choices": []any{map[string]any{"finish_reason": finish, "message": map[string]string{"content": content}}}})
	require.NoError(t, err)
	return string(body)
}

type failedReader struct{}

func (failedReader) Read([]byte) (int, error) { return 0, errors.New("secret source read failed") }

func TestExtractPlaintext(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name, text, contentType string
		wantErr                 bool
	}{
		{name: "text", text: "First line\nSecond line\n", contentType: "text/plain"},
		{name: "parameters", text: "Café", contentType: "text/plain; charset=utf-8"},
		{name: "empty", contentType: "text/plain"},
		{name: "invalid UTF8", text: "\xff", contentType: "text/plain", wantErr: true},
		{name: "invalid MIME", text: "secret", contentType: "text/plain;secret", wantErr: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			client, err := lmstudio.New(lmstudio.Config{URL: "http://localhost:1", Model: "test"})
			require.NoError(t, err)
			got, err := client.Extract(t.Context(), strings.NewReader(tt.text), tt.contentType)
			if tt.wantErr {
				require.ErrorIs(t, err, ocr.ErrInvalidDocument)
				require.Empty(t, got)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.text, got)
		})
	}
}

func TestExtractLive(t *testing.T) {
	endpoint := os.Getenv("LMSTUDIO_OCR_TEST_URL")
	if endpoint == "" {
		t.Skip("set LMSTUDIO_OCR_TEST_URL to test the loaded olmOCR model")
	}
	page := image.NewRGBA(image.Rect(0, 0, 320, 90))
	draw.Draw(page, page.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	writer := font.Drawer{Dst: page, Src: image.NewUniform(color.Black), Face: basicfont.Face7x13, Dot: fixed.P(12, 30)}
	writer.DrawString("NAUTILUS OCR TEST")
	writer.Dot = fixed.P(12, 60)
	writer.DrawString("Reference: Q7M4-9281")
	large := image.NewRGBA(image.Rect(0, 0, 1280, 360))
	for y := range 360 {
		for x := range 1280 {
			large.Set(x, y, page.At(x/4, y/4))
		}
	}
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, large))
	client, err := lmstudio.New(lmstudio.Config{URL: endpoint, Model: "allenai/olmocr-2-7b"})
	require.NoError(t, err)
	text, err := client.Extract(t.Context(), &buf, "image/png")
	require.NoError(t, err)
	require.Contains(t, text, "NAUTILUS OCR TEST")
	require.Contains(t, text, "Q7M4-9281")
	require.NotContains(t, text, "primary_language:")
}
