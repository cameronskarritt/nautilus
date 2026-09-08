package opensearch

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"nautilus/internal/errors"
)

type Config struct {
	URL      string
	Index    string
	Username string
	Password string
}

type Client struct {
	url      string
	index    string
	username string
	password string
	http     *http.Client
}

var indexName = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,254}$`)

func New(cfg Config) (*Client, error) {
	u, err := url.Parse(cfg.URL)
	if err != nil || u == nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.Contains(cfg.URL, "#") || (u.Path != "" && u.Path != "/") || u.RawPath != "" {
		return nil, errors.New("invalid OpenSearch URL: expected an HTTP(S) origin without credentials, query, or fragment")
	}
	if !indexName.MatchString(cfg.Index) {
		return nil, errors.New("invalid OpenSearch index: expected 1–255 lowercase ASCII letters, digits, dots, underscores, or hyphens, starting with a letter or digit")
	}
	if strings.Contains(cfg.Username, ":") || (cfg.Username == "" && cfg.Password != "") {
		return nil, errors.New("invalid OpenSearch basic authentication credentials")
	}
	return &Client{
		url:      strings.TrimSuffix(u.String(), "/"),
		index:    cfg.Index,
		username: cfg.Username,
		password: cfg.Password,
		http: &http.Client{
			Timeout: 10 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}, nil
}

const indexMapping = `{"mappings":{"dynamic":"strict","properties":{"organization_id":{"type":"keyword"},"document_id":{"type":"keyword"},"text":{"type":"text"}}}}`

func (c *Client) EnsureIndex(ctx context.Context) error {
	status, body, err := c.request(ctx, http.MethodPut, "", strings.NewReader(indexMapping))
	if err != nil {
		return err
	}
	if status == http.StatusOK {
		var result struct {
			Acknowledged bool `json:"acknowledged"`
		}
		if json.Unmarshal(body, &result) != nil || !result.Acknowledged {
			return errors.New("OpenSearch index creation was not acknowledged")
		}
		return nil
	}
	var result struct {
		Error struct {
			Type string `json:"type"`
		} `json:"error"`
	}
	if status != http.StatusBadRequest || json.Unmarshal(body, &result) != nil || result.Error.Type != "resource_already_exists_exception" {
		return errors.Errorf("OpenSearch index creation returned HTTP %d", status)
	}
	status, body, err = c.request(ctx, http.MethodGet, "/_mapping", nil)
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		return errors.Errorf("OpenSearch mapping lookup returned HTTP %d", status)
	}
	var indexes map[string]struct {
		Mappings struct {
			Dynamic    string `json:"dynamic"`
			Properties map[string]struct {
				Type string `json:"type"`
			} `json:"properties"`
		} `json:"mappings"`
	}
	if json.Unmarshal(body, &indexes) != nil {
		return errors.New("invalid OpenSearch mapping response")
	}
	mapping := indexes[c.index].Mappings
	if len(indexes) != 1 || mapping.Dynamic != "strict" || len(mapping.Properties) != 3 || mapping.Properties["organization_id"].Type != "keyword" || mapping.Properties["document_id"].Type != "keyword" || mapping.Properties["text"].Type != "text" {
		return errors.New("OpenSearch index mapping is incompatible")
	}
	return nil
}

func (c *Client) request(ctx context.Context, method, path string, body io.Reader) (int, []byte, error) {
	return c.requestLimit(ctx, method, path, body, 1<<20)
}

func (c *Client) requestLimit(ctx context.Context, method, path string, body io.Reader, maxBody int64) (int, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.url+"/"+c.index+path, body)
	if err != nil {
		return 0, nil, errors.New("invalid OpenSearch request")
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.username != "" {
		req.SetBasicAuth(c.username, c.password)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return 0, nil, errors.Wrap(ctx.Err(), "OpenSearch request interrupted")
		}
		// Transport errors can contain URLs and credentials. Keep them private.
		return 0, nil, errors.New("OpenSearch request failed")
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil {
		if ctx.Err() != nil {
			return 0, nil, errors.Wrap(ctx.Err(), "OpenSearch response interrupted")
		}
		return 0, nil, errors.New("failed to read OpenSearch response")
	}
	if int64(len(b)) > maxBody {
		return 0, nil, errors.New("OpenSearch response exceeds size limit")
	}
	return resp.StatusCode, b, nil
}
