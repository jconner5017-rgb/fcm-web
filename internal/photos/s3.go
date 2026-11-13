package photos

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// S3 is an S3-compatible object store (Cloudflare R2, AWS S3, Backblaze B2),
// signed with AWS Signature Version 4 so no SDK is needed. Copied from Slabs
// & Singles, plus presigned links and HEAD.
type S3 struct {
	Endpoint  string // https://<account>.r2.cloudflarestorage.com
	Bucket    string
	Region    string // "auto" for R2
	AccessKey string
	SecretKey string
	Client    *http.Client
	now       func() time.Time
}

func (s *S3) objectURL(key string) string {
	if s.Bucket == "" { // virtual-hosted endpoint: the bucket is in the host name
		return strings.TrimRight(s.Endpoint, "/") + "/" + key
	}
	return strings.TrimRight(s.Endpoint, "/") + "/" + s.Bucket + "/" + key
}

// Put uploads an object (used by tests and server-side jobs; devices
// upload through presigned links instead).
func (s *S3) Put(ctx context.Context, key, contentType string, data []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, s.objectURL(key), bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", contentType)
	return s.do(req, data)
}

// Head returns an object's size, or found=false when it doesn't exist.
func (s *S3) Head(ctx context.Context, key string) (size int64, found bool, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, s.objectURL(key), nil)
	if err != nil {
		return 0, false, err
	}
	s.sign(req, nil, s.clock().UTC())
	res, err := s.client().Do(req)
	if err != nil {
		return 0, false, err
	}
	res.Body.Close()
	switch {
	case res.StatusCode == http.StatusNotFound:
		return 0, false, nil
	case res.StatusCode >= 300:
		return 0, false, fmt.Errorf("photo storage HEAD: %d", res.StatusCode)
	}
	return res.ContentLength, true, nil
}

// Get streams an object. The caller closes the body. found=false when it
// doesn't exist.
func (s *S3) Get(ctx context.Context, key string) (body io.ReadCloser, size int64, found bool, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.objectURL(key), nil)
	if err != nil {
		return nil, 0, false, err
	}
	s.sign(req, nil, s.clock().UTC())
	res, err := s.client().Do(req)
	if err != nil {
		return nil, 0, false, err
	}
	switch {
	case res.StatusCode == http.StatusNotFound:
		res.Body.Close()
		return nil, 0, false, nil
	case res.StatusCode >= 300:
		res.Body.Close()
		return nil, 0, false, fmt.Errorf("photo storage GET: %d", res.StatusCode)
	}
	return res.Body, res.ContentLength, true, nil
}

func (s *S3) clock() time.Time {
	if s.now != nil {
		return s.now()
	}
	return time.Now()
}

func (s *S3) client() *http.Client {
	if s.Client != nil {
		return s.Client
	}
	return &http.Client{Timeout: 30 * time.Second}
}

// Presign returns a link that allows one method (GET or PUT) on one object
// until it expires, with no other credentials: AWS Signature Version 4 in
// the query string, payload unsigned. Devices upload with it and browsers
// show photos with it, so photo bytes never pass through the API server.
func (s *S3) Presign(method, key string, expires time.Duration, now time.Time) string {
	u, _ := url.Parse(s.objectURL(key))
	now = now.UTC()
	amzDate := now.Format("20060102T150405Z")
	day := now.Format("20060102")
	scope := day + "/" + s.Region + "/s3/aws4_request"
	q := url.Values{}
	q.Set("X-Amz-Algorithm", "AWS4-HMAC-SHA256")
	q.Set("X-Amz-Credential", s.AccessKey+"/"+scope)
	q.Set("X-Amz-Date", amzDate)
	q.Set("X-Amz-Expires", fmt.Sprint(int(expires.Seconds())))
	q.Set("X-Amz-SignedHeaders", "host")
	canonical := strings.Join([]string{
		method,
		canonicalPath(u.Path),
		canonicalQuery(q),
		"host:" + u.Host + "\n",
		"host",
		"UNSIGNED-PAYLOAD",
	}, "\n")
	toSign := "AWS4-HMAC-SHA256\n" + amzDate + "\n" + scope + "\n" + sha([]byte(canonical))
	k := hmacSHA([]byte("AWS4"+s.SecretKey), day)
	k = hmacSHA(k, s.Region)
	k = hmacSHA(k, "s3")
	k = hmacSHA(k, "aws4_request")
	u.RawQuery = canonicalQuery(q) + "&X-Amz-Signature=" + hex.EncodeToString(hmacSHA(k, toSign))
	return u.String()
}

// Delete removes an object. A missing object is not an error.
func (s *S3) Delete(ctx context.Context, key string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, s.objectURL(key), nil)
	if err != nil {
		return err
	}
	return s.do(req, nil)
}

func (s *S3) do(req *http.Request, body []byte) error {
	s.sign(req, body, s.clock().UTC())
	res, err := s.client().Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 && !(req.Method == http.MethodDelete && res.StatusCode == http.StatusNotFound) {
		msg, _ := io.ReadAll(io.LimitReader(res.Body, 500))
		return fmt.Errorf("photo storage %s: %d %s", req.Method, res.StatusCode, strings.TrimSpace(string(msg)))
	}
	return nil
}

func sha(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func hmacSHA(key []byte, s string) []byte {
	m := hmac.New(sha256.New, key)
	m.Write([]byte(s))
	return m.Sum(nil)
}

// sign adds SigV4 headers (x-amz-date, x-amz-content-sha256, Authorization).
// Every header already set on the request is signed, plus host.
func (s *S3) sign(req *http.Request, body []byte, now time.Time) {
	amzDate := now.Format("20060102T150405Z")
	day := now.Format("20060102")
	payload := sha(body)
	req.Header.Set("x-amz-date", amzDate)
	req.Header.Set("x-amz-content-sha256", payload)

	headers := map[string]string{"host": req.URL.Host}
	for k, v := range req.Header {
		headers[strings.ToLower(k)] = strings.TrimSpace(strings.Join(v, ","))
	}
	names := make([]string, 0, len(headers))
	for k := range headers {
		names = append(names, k)
	}
	sort.Strings(names)
	var canonHeaders strings.Builder
	for _, k := range names {
		canonHeaders.WriteString(k + ":" + headers[k] + "\n")
	}
	signed := strings.Join(names, ";")

	canonical := strings.Join([]string{
		req.Method,
		canonicalPath(req.URL.Path),
		canonicalQuery(req.URL.Query()),
		canonHeaders.String(),
		signed,
		payload,
	}, "\n")
	scope := day + "/" + s.Region + "/s3/aws4_request"
	toSign := "AWS4-HMAC-SHA256\n" + amzDate + "\n" + scope + "\n" + sha([]byte(canonical))
	k := hmacSHA([]byte("AWS4"+s.SecretKey), day)
	k = hmacSHA(k, s.Region)
	k = hmacSHA(k, "s3")
	k = hmacSHA(k, "aws4_request")
	sig := hex.EncodeToString(hmacSHA(k, toSign))
	req.Header.Set("Authorization", fmt.Sprintf("AWS4-HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		s.AccessKey, scope, signed, sig))
}

func canonicalPath(p string) string {
	if p == "" {
		return "/"
	}
	parts := strings.Split(p, "/")
	for i, seg := range parts {
		parts[i] = uriEncode(seg)
	}
	return strings.Join(parts, "/")
}

func canonicalQuery(q url.Values) string {
	keys := make([]string, 0, len(q))
	for k := range q {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var out []string
	for _, k := range keys {
		vs := q[k]
		sort.Strings(vs)
		for _, v := range vs {
			out = append(out, uriEncode(k)+"="+uriEncode(v))
		}
	}
	return strings.Join(out, "&")
}

// uriEncode is RFC 3986 encoding as SigV4 requires (unreserved chars kept).
func uriEncode(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' || c == '_' || c == '.' || c == '~' {
			b.WriteByte(c)
		} else {
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}
