// Copyright (c) Relyance, Inc.
// SPDX-License-Identifier: MPL-2.0

package common

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
)

// ErrNotFound is returned when the API responds 404; callers use errors.Is to
// map it onto Terraform's "resource no longer exists" (RemoveResource) flow.
var ErrNotFound = errors.New("not found")
var (
	jwtPattern          = regexp.MustCompile(`\beyJ[A-Za-z0-9_-]+\.[A-Za-z0-9._-]+\.[A-Za-z0-9._-]*\b`)
	bearerPattern       = regexp.MustCompile(`\b[Bb]earer\s+[A-Za-z0-9._~+/\-]+=*\b`)
	apiKeyPattern       = regexp.MustCompile(`\b(?:api[_-]?key|access[_-]?token|secret[_-]?key|private[_-]?key|client[_-]?secret)["\s:=]+[A-Za-z0-9._~+/\-]{16,}\b`)
	emailInCredsPattern = regexp.MustCompile(`"email"\s*:\s*"[^"]+@[^"]+"`)
)

type problem struct {
	Type   string `json:"type,omitempty"`
	Title  string `json:"title,omitempty"`
	Detail string `json:"detail,omitempty"`
	Status int    `json:"status,omitempty"`
	Code   string `json:"code,omitempty"`
}

// ErrWithHTTP enriches err with request/response context (method, URL, status,
// correlation id) and, for text-ish bodies, a sanitized+truncated snippet. Secret
// material (bearer tokens, JWTs, api keys, emails) is redacted via sanitize before
// it can reach a diagnostic or log.
func ErrWithHTTP(r *http.Response, err error) error {
	if r == nil {
		return err
	}

	method, url := "", ""
	if r.Request != nil {
		method = r.Request.Method
		if r.Request.URL != nil {
			url = r.Request.URL.String()
		}
	}

	ct := r.Header.Get("Content-Type")
	reqID := firstNonEmpty(r.Header.Get("X-Correlation-Id"), r.Header.Get("X-Request-Id"), r.Header.Get("Trace-Id"))
	retryAfter := r.Header.Get("Retry-After")

	// Default message with no body.
	msg := fmt.Sprintf("%s %s -> HTTP %d %s ct=%s", method, url, r.StatusCode, r.Status, ct)
	if reqID != "" {
		msg += " req_id=" + reqID
	}
	if retryAfter != "" {
		msg += " retry-after=" + retryAfter
	}

	// Only consider body for text-ish content.
	if strings.Contains(ct, "application/problem+json") || strings.Contains(ct, "application/json") || strings.Contains(ct, "text/") {
		const limit = 8 << 10 // 8KB
		b, _ := io.ReadAll(io.LimitReader(r.Body, limit))
		// Always close to avoid leaks.
		_, _ = io.Copy(io.Discard, r.Body)
		_ = r.Body.Close()

		if len(b) > 0 {
			if strings.Contains(ct, "application/problem+json") {
				var p problem
				if json.Unmarshal(b, &p) == nil {
					p.Detail = sanitize(p.Detail)
					msg += fmt.Sprintf(` problem{title=%q detail=%q code=%q type=%q}`, p.Title, p.Detail, p.Code, p.Type)
					return fmt.Errorf("%w (%s)", err, msg)
				}
			}
			body := sanitize(string(b))
			if len(body) > 500 {
				body = body[:500] + "...[TRUNCATED]"
			}
			msg += " body=" + body
		}
	} else if r.Body != nil {
		// Ensure body is closed even if we didn't read it.
		_, _ = io.Copy(io.Discard, r.Body)
		_ = r.Body.Close()
	}

	return fmt.Errorf("%w (%s)", err, msg)
}

func firstNonEmpty(vs ...string) string {
	for _, v := range vs {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// Sanitize redacts secret material (bearer tokens, JWTs, api keys, client
// secrets, emails) from an arbitrary string before it reaches a diagnostic or
// log. Exported for callers outside this package (e.g. the OAuth token source)
// that build error messages from raw HTTP bodies.
func Sanitize(s string) string { return sanitize(s) }

// sanitize does minimal redaction; keep it conservative.
func sanitize(s string) string {
	s = bearerPattern.ReplaceAllString(s, "Bearer [REDACTED]")
	s = jwtPattern.ReplaceAllString(s, "[REDACTED_JWT]")
	s = apiKeyPattern.ReplaceAllString(s, "[REDACTED_API_KEY]")
	// Optional: redact emails if you often echo credential payloads
	s = emailInCredsPattern.ReplaceAllString(s, `"email":"[REDACTED]"`)
	return s
}
