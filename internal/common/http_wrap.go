// Copyright (c) Relyance, Inc.
// SPDX-License-Identifier: MPL-2.0

// Package common provides shared HTTP transport wrappers (user-agent injection and
// retry-with-backoff) and RFC-7807-aware error enrichment that redacts secrets. These
// helpers are transport-level and hold no provider state.
package common

import (
	"bytes"
	"fmt"
	"io"
	"math"
	"math/rand"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// UserAgentRoundTripper sets a default User-Agent and negotiates a JSON Accept
// header on outbound requests, delegating to Base.
type UserAgentRoundTripper struct {
	Base http.RoundTripper
	UA   string
}

// RoundTrip adds the User-Agent/Accept/Content-Type defaults and calls Base.
func (u UserAgentRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) {
	r2 := r.Clone(r.Context())
	if r2.Header.Get("User-Agent") == "" {
		r2.Header.Set("User-Agent", u.UA)
	}
	ah := r2.Header.Get("Accept")
	if ah == "" || ah == "*/*" {
		r2.Header.Set("Accept", "application/json")
	} else if !strings.Contains(ah, "application/json") {
		r2.Header.Set("Accept", ah+", application/json")
	}
	if r2.Body != nil && r2.Header.Get("Content-Type") == "" {
		r2.Header.Set("Content-Type", "application/json")
	}
	return u.Base.RoundTrip(r2)
}

// ProblemError carries an HTTP status and (already-sanitized) body for surfacing
// a server error as a Go error.
type ProblemError struct {
	Status string
	Body   string
}

// Error renders the status and body.
func (p ProblemError) Error() string {
	return fmt.Sprintf("%s: %s", p.Status, p.Body)
}

// RetryRoundTripper retries 429/5xx responses with exponential backoff and jitter,
// honoring a Retry-After header (seconds or HTTP-date) when RespectHeaders is set.
type RetryRoundTripper struct {
	Base           http.RoundTripper
	Max            int64
	WaitMin        time.Duration
	WaitMax        time.Duration
	RespectHeaders bool
}

// RoundTrip performs the request, retrying transient failures up to Max times.
func (r RetryRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	reqClone := req.Clone(req.Context())
	bodyReset, err := snapshotBody(reqClone)
	if err != nil {
		return nil, err
	}
	attempts := int64(0)
	for {
		if err := bodyReset(reqClone); err != nil {
			return nil, err
		}
		resp, err := r.Base.RoundTrip(reqClone)
		if !shouldRetry(reqClone.Method, resp, err) || attempts >= r.Max {
			return resp, err
		}
		// Compute the wait — which may read Retry-After off resp — before discarding it.
		wait := r.backoff(attempts, resp)
		drainBody(resp)
		resp = nil
		timer := time.NewTimer(wait)
		select {
		case <-reqClone.Context().Done():
			timer.Stop()
			if err == nil {
				err = reqClone.Context().Err()
			}
			return resp, err
		case <-timer.C:
		}
		attempts++
	}
}

func shouldRetry(method string, resp *http.Response, err error) bool {
	// 429 is safe to retry for any method: the server rejected the request before
	// processing it, so there is no side effect to duplicate.
	if resp != nil && resp.StatusCode == http.StatusTooManyRequests {
		return true
	}
	// A network timeout or a 5xx may mean the server already processed the request,
	// so only auto-retry idempotent methods -- never blind-retry a POST that could
	// have created a resource (e.g. a connection or a connect attempt).
	if !isIdempotent(method) {
		return false
	}
	if err != nil {
		if urlErr, ok := err.(urlErrorLike); ok && urlErr.Timeout() {
			return true
		}
	}
	if resp == nil {
		return false
	}
	if resp.StatusCode >= 500 && resp.StatusCode != 501 && resp.StatusCode != 505 {
		return true
	}
	return false
}

// isIdempotent reports whether a method is safe to auto-retry after a timeout or
// 5xx. POST and PATCH are excluded: retrying them could duplicate a create or
// re-apply a non-idempotent change.
func isIdempotent(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodPut, http.MethodDelete, http.MethodOptions, http.MethodTrace:
		return true
	default:
		return false
	}
}

type urlErrorLike interface{ Timeout() bool }

func (r RetryRoundTripper) backoff(attempt int64, resp *http.Response) time.Duration {
	// Retry-After header takes precedence
	if r.RespectHeaders && resp != nil {
		if ra := strings.TrimSpace(resp.Header.Get("Retry-After")); ra != "" {
			// seconds?
			if n, err := strconv.ParseInt(ra, 10, 64); err == nil && n >= 0 {
				return clamp(time.Duration(n)*time.Second, r.WaitMin, r.WaitMax)
			}
			// HTTP-date?
			if t, err := http.ParseTime(ra); err == nil {
				d := time.Until(t)
				if d > 0 {
					return clamp(d, r.WaitMin, r.WaitMax)
				}
			}
		}
	}
	// exponential with jitter
	base := float64(r.WaitMin)
	mult := math.Pow(2, float64(attempt))
	jitter := 0.5 + rand.Float64() // 0.5x..1.5x
	wait := time.Duration(base * mult * jitter)
	return clamp(wait, r.WaitMin, r.WaitMax)
}

func clamp(d, min, max time.Duration) time.Duration {
	if d < min {
		return min
	}
	if max > 0 && d > max {
		return max
	}
	return d
}

type bodyResetFunc func(*http.Request) error

func snapshotBody(req *http.Request) (bodyResetFunc, error) {
	if req.Body == nil {
		return func(*http.Request) error { return nil }, nil
	}
	if req.GetBody != nil {
		return func(r *http.Request) error {
			body, err := req.GetBody()
			if err != nil {
				return err
			}
			r.Body = body
			return nil
		}, nil
	}
	buf, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	if err := req.Body.Close(); err != nil {
		return nil, err
	}
	return func(r *http.Request) error {
		r.Body = io.NopCloser(bytes.NewReader(buf))
		return nil
	}, nil
}

func drainBody(resp *http.Response) {
	if resp == nil || resp.Body == nil {
		return
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
}
