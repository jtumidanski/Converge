package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// DoJSON performs req, maps non-2xx statuses to *StatusError, and decodes JSON into out.
func DoJSON(ctx context.Context, client *http.Client, req *http.Request, out any) (http.Header, error) {
	resp, err := client.Do(req.WithContext(ctx))
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w", req.Method, req.URL.Path, ErrUnavailable)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
		return resp.Header, NewStatusError(req.Method, req.URL.Path, resp.StatusCode, resp.Header)
	}
	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			return resp.Header, fmt.Errorf("%s %s: decode response: %w", req.Method, req.URL.Path, ErrUnavailable)
		}
	}
	return resp.Header, nil
}
