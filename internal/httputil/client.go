package httputil

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"

	"nautilus/internal/errors"
)

type Client struct {
	client *http.Client
	Header http.Header
}

func NewClient() *Client {
	header := make(http.Header)
	header.Add("Content-Type", "application/json")

	return &Client{
		client: http.DefaultClient,
		Header: header,
	}
}

func (c *Client) makeRequest(ctx context.Context, method string, endpoint string, payload any, response any) (*http.Response, error) {
	var body io.Reader
	if payload != nil {
		buf, err := json.Marshal(payload)
		if err != nil {
			return nil, errors.Wrap(err, "error marshaling payload")
		}
		body = bytes.NewReader(buf)
	}

	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return nil, errors.Wrap(err, "error creating request")
	}

	req.Header = c.Header

	raw, err := c.client.Do(req)
	if err != nil {
		return nil, errors.Wrap(err, "error making request")
	}
	defer raw.Body.Close()

	err = json.NewDecoder(raw.Body).Decode(response)
	if err != nil {
		return raw, errors.Wrap(err, "error decoding response body")
	}

	return raw, nil
}

func (c *Client) Get(ctx context.Context, endpoint string, response any) (*http.Response, error) {
	return c.makeRequest(ctx, http.MethodGet, endpoint, nil, response)
}

func (c *Client) Post(ctx context.Context, endpoint string, payload any, response any) (*http.Response, error) {
	return c.makeRequest(ctx, http.MethodPost, endpoint, payload, response)
}

func (c *Client) Put(ctx context.Context, endpoint string, payload any, response any) (*http.Response, error) {
	return c.makeRequest(ctx, http.MethodPut, endpoint, payload, response)
}

func (c *Client) Patch(ctx context.Context, endpoint string, payload any, response any) (*http.Response, error) {
	return c.makeRequest(ctx, http.MethodPatch, endpoint, payload, response)
}

func (c *Client) Delete(ctx context.Context, endpoint string, response any) (*http.Response, error) {
	return c.makeRequest(ctx, http.MethodDelete, endpoint, nil, response)
}
