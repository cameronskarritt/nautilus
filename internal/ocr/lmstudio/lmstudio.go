package lmstudio

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"nautilus/internal/errors"
	"nautilus/internal/httputil"
	"nautilus/internal/ocr"
)

const maxDocumentBytes = 100 << 20

// The olmOCR v4 inference prompt: https://github.com/allenai/olmocr/blob/main/olmocr/prompts/prompts.py.
const prompt = "Attached is one page of a document that you must process. " +
	"Just return the plain text representation of this document as if you were reading it naturally. Convert equations to LateX and tables to HTML.\n" +
	"If there are any figures or charts, label them with the following markdown syntax ![Alt text describing the contents of the figure](page_startx_starty_width_height.png)\n" +
	"Return your output as markdown, with a front matter section on top specifying values for the primary_language, is_rotation_valid, rotation_correction, is_table, and is_diagram parameters."

type Config struct {
	URL    string
	Model  string
	APIKey string
}

type Client struct {
	endpoint string
	model    string
	apiKey   string
	http     *http.Client
}

var _ ocr.OCR = (*Client)(nil)

func New(cfg Config) (*Client, error) {
	u := httputil.ParseBaseURL(cfg.URL)
	if u == nil {
		return nil, errors.New("invalid OCR base URL")
	}
	if strings.TrimSpace(cfg.Model) == "" || !utf8.ValidString(cfg.Model) || len(cfg.Model) > 512 {
		return nil, errors.New("OCR model is required and must be at most 512 UTF-8 bytes")
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/chat/completions"
	return &Client{
		endpoint: u.String(), model: cfg.Model, apiKey: cfg.APIKey,
		http: &http.Client{
			Timeout:       90 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	}, nil
}

func (c *Client) Extract(ctx context.Context, data io.Reader, contentType string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", errors.Wrap(err, "OCR canceled")
	}
	if data == nil {
		return "", errors.Wrap(ocr.ErrInvalidDocument, "OCR input is missing")
	}
	body, err := io.ReadAll(io.LimitReader(data, maxDocumentBytes+1))
	defer clear(body)
	if err != nil {
		if ctx.Err() != nil {
			return "", errors.Wrap(ctx.Err(), "OCR canceled")
		}
		return "", errors.New("read OCR input")
	}
	if len(body) > maxDocumentBytes {
		return "", errors.Wrap(ocr.ErrInvalidDocument, "OCR input exceeds byte limit")
	}
	if err := ctx.Err(); err != nil {
		return "", errors.Wrap(err, "OCR canceled")
	}
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return "", errors.Wrap(ocr.ErrInvalidDocument, "invalid OCR content type")
	}
	if mediaType == "text/plain" {
		if !utf8.Valid(body) {
			return "", errors.Wrap(ocr.ErrInvalidDocument, "OCR plaintext must be UTF-8")
		}
		return string(body), nil
	}
	var text strings.Builder
	pages := 0
	err = render(ctx, body, mediaType, func(page []byte) error {
		part, err := c.extractPage(ctx, page)
		if err != nil {
			return err
		}
		separator := ""
		if pages > 0 {
			separator = "\n"
		}
		if text.Len()+len(separator)+len(part) > maxDocumentBytes {
			return errors.Wrap(ocr.ErrInvalidDocument, "OCR output exceeds byte limit")
		}
		text.WriteString(separator)
		text.WriteString(part)
		pages++
		return nil
	})
	if err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", errors.Wrap(err, "OCR canceled")
	}
	return text.String(), nil
}

func (c *Client) extractPage(ctx context.Context, page []byte) (string, error) {
	body, err := json.Marshal(map[string]any{
		"model": c.model, "temperature": 0, "max_tokens": 4096, "stream": false,
		"messages": []any{map[string]any{
			"role": "user", "content": []any{
				map[string]any{"type": "text", "text": prompt},
				map[string]any{"type": "image_url", "image_url": map[string]string{"url": "data:image/png;base64," + base64.StdEncoding.EncodeToString(page)}},
			},
		}},
	})
	if err != nil {
		return "", errors.New("encode OCR request")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return "", errors.New("create OCR request")
	}
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return "", errors.Wrap(ctx.Err(), "OCR canceled")
		}
		return "", errors.New("OCR request failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", errors.Errorf("OCR request returned HTTP %d", resp.StatusCode)
	}
	body, err = io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
	if err != nil {
		if ctx.Err() != nil {
			return "", errors.Wrap(ctx.Err(), "OCR canceled")
		}
		return "", errors.New("read OCR response")
	}
	if len(body) > 1<<20 || !utf8.Valid(body) {
		return "", errors.New("OCR response exceeds byte limit or contains invalid UTF-8")
	}
	var result struct {
		Model   string `json:"model"`
		Choices []struct {
			FinishReason string `json:"finish_reason"`
			Message      struct {
				Content *string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if json.Unmarshal(body, &result) != nil || len(result.Choices) != 1 || result.Choices[0].Message.Content == nil {
		return "", errors.New("invalid OCR response")
	}
	if result.Model != "" && result.Model != c.model {
		return "", errors.New("OCR response model mismatch")
	}
	if result.Choices[0].FinishReason == "length" {
		return "", errors.Wrap(ocr.ErrInvalidDocument, "OCR page exceeds output token limit")
	}
	if result.Choices[0].FinishReason != "stop" {
		return "", errors.New("OCR response did not finish completely")
	}
	return parsePage(*result.Choices[0].Message.Content)
}

// olmOCR emits five scalar fields, not arbitrary YAML. Reject unfamiliar or
// incomplete frontmatter so malformed model output never becomes document text.
func parsePage(content string) (string, error) {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	if !strings.HasPrefix(content, "---\n") {
		return "", errors.New("OCR response is missing frontmatter")
	}
	header, text, ok := strings.Cut(content[4:], "\n---")
	if !ok || (text != "" && !strings.HasPrefix(text, "\n")) {
		return "", errors.New("invalid OCR frontmatter boundary")
	}
	fields := make(map[string]string, 5)
	for line := range strings.SplitSeq(header, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		key, value, ok := strings.Cut(line, ":")
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)
		if !ok || fields[key] != "" || value == "" {
			return "", errors.New("invalid OCR frontmatter field")
		}
		switch key {
		case "primary_language":
			language := value
			if len(language) >= 2 && ((language[0] == '\'' && language[len(language)-1] == '\'') || (language[0] == '"' && language[len(language)-1] == '"')) {
				language = language[1 : len(language)-1]
			}
			if language != "null" {
				if len(language) < 2 || len(language) > 16 {
					return "", errors.New("invalid OCR language metadata")
				}
				for _, ch := range language {
					if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || ch == '-' {
						continue
					}
					return "", errors.New("invalid OCR language metadata")
				}
			}
		case "is_rotation_valid", "is_table", "is_diagram":
			switch value {
			case "true", "True", "TRUE", "false", "False", "FALSE":
				value = strings.ToLower(value)
			default:
				return "", errors.New("invalid OCR boolean metadata")
			}
		case "rotation_correction":
			if value != "0" && value != "90" && value != "180" && value != "270" {
				return "", errors.New("invalid OCR rotation metadata")
			}
		default:
			return "", errors.New("unknown OCR frontmatter field")
		}
		fields[key] = value
	}
	if len(fields) != 5 {
		return "", errors.New("incomplete OCR frontmatter")
	}
	if fields["is_rotation_valid"] != "true" || fields["rotation_correction"] != "0" {
		return "", errors.Wrap(ocr.ErrInvalidDocument, "OCR page requires unsupported rotation")
	}
	return strings.TrimSpace(text), nil
}
