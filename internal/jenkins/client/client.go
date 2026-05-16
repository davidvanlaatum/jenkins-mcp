package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/david/jenkins-mcp/internal/config"
	apperrors "github.com/david/jenkins-mcp/internal/errors"
)

type Client struct {
	base      *url.URL
	username  string
	token     string
	http      *http.Client
	jar       *resettableCookieJar
	logger    *slog.Logger
	crumb     *Crumb
	crumbLock sync.Mutex
}
type Crumb struct {
	RequestField string `json:"crumbRequestField"`
	Crumb        string `json:"crumb"`
}

func New(cfg config.ControllerConfig, logger *slog.Logger) (*Client, error) {
	parsed, err := url.Parse(strings.TrimRight(cfg.URL, "/"))
	if err != nil {
		return nil, err
	}
	if logger != nil {
		logger.Debug("configured Jenkins controller", "controller", cfg.ID, "base_url", parsed.Redacted(), "base_path", parsed.EscapedPath())
	}
	jar, err := newResettableCookieJar()
	if err != nil {
		return nil, err
	}
	return &Client{base: parsed, username: cfg.Username, token: cfg.Token, http: &http.Client{Timeout: 30 * time.Second, Jar: jar}, jar: jar, logger: logger}, nil
}
func (c *Client) BaseURL() string { return c.base.String() }

func (c *Client) GetJSON(ctx context.Context, path string, query url.Values, out any) error {
	status, body, headers, err := c.Do(ctx, http.MethodGet, path, query, nil, nil)
	if err != nil {
		return err
	}
	if status < 200 || status > 299 {
		return classify(status, string(body))
	}
	if version := headers.Get("X-Jenkins"); version != "" {
		if m, ok := out.(interface{ SetVersion(string) }); ok {
			m.SetVersion(version)
		}
	}
	if err := json.Unmarshal(body, out); err != nil {
		return apperrors.Wrap(apperrors.CodeJenkins, "invalid Jenkins JSON response", err.Error())
	}
	return nil
}

func (c *Client) GetText(ctx context.Context, path string, query url.Values) (int, []byte, http.Header, error) {
	return c.Do(ctx, http.MethodGet, path, query, nil, nil)
}

func (c *Client) Post(ctx context.Context, path string, query url.Values, form url.Values) (int, []byte, http.Header, error) {
	encodedForm := ""
	if form != nil {
		encodedForm = form.Encode()
	}
	status, body, headers, err := c.postOnce(ctx, path, query, form != nil, encodedForm)
	if err != nil || status != http.StatusForbidden {
		return status, body, headers, err
	}
	c.clearCrumbSession()
	return c.postOnce(ctx, path, query, form != nil, encodedForm)
}

func (c *Client) postOnce(ctx context.Context, path string, query url.Values, hasForm bool, encodedForm string) (int, []byte, http.Header, error) {
	headers := http.Header{}
	if err := c.addCrumb(ctx, headers); err != nil {
		return 0, nil, nil, err
	}
	var body io.Reader
	if hasForm {
		body = strings.NewReader(encodedForm)
		headers.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	return c.Do(ctx, http.MethodPost, path, query, body, headers)
}

func (c *Client) Do(ctx context.Context, method, path string, query url.Values, body io.Reader, headers http.Header) (int, []byte, http.Header, error) {
	u, err := c.endpointURL(path, query)
	if err != nil {
		return 0, nil, nil, err
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), body)
	if err != nil {
		return 0, nil, nil, err
	}
	if c.username != "" || c.token != "" {
		req.SetBasicAuth(c.username, c.token)
	}
	for k, vals := range headers {
		for _, v := range vals {
			req.Header.Add(k, v)
		}
	}
	started := time.Now()
	if c.logger != nil {
		c.logger.Debug("sending Jenkins request", "method", method, "url", req.URL.Redacted(), "base_url", c.base.Redacted(), "request_path", path, "has_body", body != nil)
	}
	res, err := c.http.Do(req)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return 0, nil, nil, err
		}
		if c.logger != nil {
			c.logger.Warn("Jenkins request failed", "method", method, "url", req.URL.Redacted(), "duration_ms", time.Since(started).Milliseconds(), "error", err)
		}
		return 0, nil, nil, apperrors.Wrap(apperrors.CodeUnavailable, "Jenkins request failed", err.Error())
	}
	defer func() { _ = res.Body.Close() }()
	b, err := readBounded(res.Body, 8*1024*1024)
	if err != nil {
		return 0, nil, nil, err
	}
	if c.logger != nil {
		c.logger.Debug("completed Jenkins request", "method", method, "url", req.URL.Redacted(), "status", res.StatusCode, "duration_ms", time.Since(started).Milliseconds(), "bytes", len(b), "jenkins_version", res.Header.Get("X-Jenkins"))
	}
	return res.StatusCode, b, res.Header, nil
}

func (c *Client) endpointURL(path string, query url.Values) (*url.URL, error) {
	escapedPath := strings.TrimRight(c.base.EscapedPath(), "/") + "/" + strings.TrimLeft(path, "/")
	decodedPath, err := url.PathUnescape(escapedPath)
	if err != nil {
		return nil, apperrors.Wrap(apperrors.CodeInvalidRequest, "invalid Jenkins request path", map[string]any{"path": path})
	}

	u := *c.base
	u.Path = decodedPath
	u.RawPath = escapedPath
	u.RawQuery = query.Encode()
	return &u, nil
}

func (c *Client) addCrumb(ctx context.Context, headers http.Header) error {
	c.crumbLock.Lock()
	defer c.crumbLock.Unlock()

	if c.crumb == nil {
		var crumb Crumb
		status, body, _, err := c.Do(ctx, http.MethodGet, "crumbIssuer/api/json", nil, nil, nil)
		if err != nil {
			return err
		}
		if status == http.StatusNotFound || status == http.StatusForbidden {
			return nil
		}
		if status < 200 || status > 299 {
			return classify(status, string(body))
		}
		if err := json.NewDecoder(bytes.NewReader(body)).Decode(&crumb); err != nil {
			return err
		}
		c.crumb = &crumb
	}
	if c.crumb.RequestField != "" && c.crumb.Crumb != "" {
		headers.Set(c.crumb.RequestField, c.crumb.Crumb)
	}
	return nil
}

func (c *Client) clearCrumbSession() {
	c.crumbLock.Lock()
	defer c.crumbLock.Unlock()

	c.crumb = nil
	if err := c.jar.Reset(); err != nil {
		if c.logger != nil {
			c.logger.Warn("failed to reset Jenkins cookie jar", "error", err)
		}
	}
}

type resettableCookieJar struct {
	mu  sync.Mutex
	jar *cookiejar.Jar
}

func newResettableCookieJar() (*resettableCookieJar, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}
	return &resettableCookieJar{jar: jar}, nil
}

func (j *resettableCookieJar) SetCookies(u *url.URL, cookies []*http.Cookie) {
	j.mu.Lock()
	defer j.mu.Unlock()

	j.jar.SetCookies(u, cookies)
}

func (j *resettableCookieJar) Cookies(u *url.URL) []*http.Cookie {
	j.mu.Lock()
	defer j.mu.Unlock()

	return j.jar.Cookies(u)
}

func (j *resettableCookieJar) Reset() error {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return err
	}
	j.mu.Lock()
	defer j.mu.Unlock()

	j.jar = jar
	return nil
}

func readBounded(reader io.Reader, maxBytes int64) ([]byte, error) {
	b, err := io.ReadAll(io.LimitReader(reader, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > maxBytes {
		return nil, apperrors.Wrap(apperrors.CodeJenkins, "Jenkins response exceeded maximum body size", map[string]any{"maxBytes": maxBytes})
	}
	return b, nil
}

func classify(status int, body string) error {
	msg := fmt.Sprintf("Jenkins returned HTTP %d", status)
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden:
		return apperrors.Wrap(apperrors.CodePermissionDenied, msg, body)
	case http.StatusNotFound:
		return apperrors.Wrap(apperrors.CodeNotFound, msg, body)
	default:
		return apperrors.Wrap(apperrors.CodeJenkins, msg, body)
	}
}
