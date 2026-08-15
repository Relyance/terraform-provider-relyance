// Copyright (c) Relyance, Inc.
// SPDX-License-Identifier: MPL-2.0

package common

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

type timeoutError struct{}

func (timeoutError) Error() string   { return "timeout" }
func (timeoutError) Timeout() bool   { return true }
func (timeoutError) Temporary() bool { return true }

func TestUserAgentRoundTripperSetsHeaders(t *testing.T) {
	var captured *http.Request
	rt := UserAgentRoundTripper{
		Base: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			captured = req
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("ok"))}, nil
		}),
		UA: "terraform-provider/1.0",
	}

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "https://example.com", io.NopCloser(strings.NewReader("{}")))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Accept", "*/*")

	_, err = rt.RoundTrip(req)
	if err != nil {
		t.Fatalf("round trip: %v", err)
	}
	if captured == nil {
		t.Fatalf("expected request forwarded to base round tripper")
	}
	if ua := captured.Header.Get("User-Agent"); ua != "terraform-provider/1.0" {
		t.Fatalf("expected user agent to be set, got %q", ua)
	}
	if accept := captured.Header.Get("Accept"); accept != "application/json" {
		t.Fatalf("expected Accept header to include application/json, got %q", accept)
	}
	if ct := captured.Header.Get("Content-Type"); ct != "application/json" {
		t.Fatalf("expected Content-Type application/json, got %q", ct)
	}
}

func TestShouldRetry(t *testing.T) {
	if !shouldRetry(http.MethodGet, nil, timeoutError{}) {
		t.Fatalf("expected timeout errors to trigger retry for GET")
	}

	resp429 := &http.Response{StatusCode: http.StatusTooManyRequests}
	if !shouldRetry(http.MethodGet, resp429, nil) {
		t.Fatalf("expected 429 to retry")
	}

	resp500 := &http.Response{StatusCode: http.StatusBadGateway}
	if !shouldRetry(http.MethodGet, resp500, nil) {
		t.Fatalf("expected 5xx to retry for GET")
	}

	resp501 := &http.Response{StatusCode: http.StatusNotImplemented}
	if shouldRetry(http.MethodGet, resp501, nil) {
		t.Fatalf("expected 501 to skip retry")
	}
}

// A POST (create/connect) must never be auto-retried on a timeout or 5xx — that
// could duplicate a server-side resource — but 429 (rejected, not processed) is safe.
func TestShouldRetryDoesNotRetryNonIdempotentPost(t *testing.T) {
	if shouldRetry(http.MethodPost, &http.Response{StatusCode: http.StatusBadGateway}, nil) {
		t.Fatalf("POST must not retry on 5xx")
	}
	if shouldRetry(http.MethodPost, nil, timeoutError{}) {
		t.Fatalf("POST must not retry on timeout")
	}
	if shouldRetry(http.MethodPatch, &http.Response{StatusCode: http.StatusServiceUnavailable}, nil) {
		t.Fatalf("PATCH must not retry on 5xx")
	}
	if !shouldRetry(http.MethodPost, &http.Response{StatusCode: http.StatusTooManyRequests}, nil) {
		t.Fatalf("POST 429 should retry (rejected before processing)")
	}
}

func TestClamp(t *testing.T) {
	min := 10 * time.Millisecond
	max := 100 * time.Millisecond
	if got := clamp(5*time.Millisecond, min, max); got != min {
		t.Fatalf("expected clamp to floor to min, got %v", got)
	}
	if got := clamp(200*time.Millisecond, min, max); got != max {
		t.Fatalf("expected clamp to cap at max, got %v", got)
	}
	if got := clamp(50*time.Millisecond, min, max); got != 50*time.Millisecond {
		t.Fatalf("expected clamp to pass through value, got %v", got)
	}
}

func TestSnapshotBodyAndDrain(t *testing.T) {
	req, err := http.NewRequest(http.MethodPost, "https://example.com", io.NopCloser(strings.NewReader("payload")))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}

	reset, err := snapshotBody(req)
	if err != nil {
		t.Fatalf("snapshot body: %v", err)
	}

	// consume body once and reset
	if _, err := io.ReadAll(req.Body); err != nil {
		t.Fatalf("read body: %v", err)
	}
	if err := reset(req); err != nil {
		t.Fatalf("reset body: %v", err)
	}
	data, err := io.ReadAll(req.Body)
	if err != nil {
		t.Fatalf("read reset body: %v", err)
	}
	if string(data) != "payload" {
		t.Fatalf("expected payload after reset, got %q", data)
	}

	resp := &http.Response{Body: io.NopCloser(strings.NewReader("discard me"))}
	drainBody(resp)
	if resp.Body != nil {
		if _, err := resp.Body.Read(make([]byte, 1)); err == nil {
			t.Fatalf("expected drained body to be closed")
		}
	}
}

func TestRetryRoundTripperRetriesUntilSuccess(t *testing.T) {
	attempts := 0
	base := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		attempts++
		if attempts == 1 {
			return &http.Response{
				StatusCode: http.StatusInternalServerError,
				Body:       io.NopCloser(strings.NewReader("fail")),
			}, nil
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader("ok")),
		}, nil
	})

	rt := RetryRoundTripper{
		Base:    base,
		Max:     3,
		WaitMin: time.Millisecond,
		WaitMax: 5 * time.Millisecond,
	}

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://example.com", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}

	resp, err := rt.RoundTrip(req)
	if err != nil {
		t.Fatalf("round trip error: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200 after retry, got %d", resp.StatusCode)
	}
	if attempts != 2 {
		t.Fatalf("expected two attempts, got %d", attempts)
	}
}

// A 429 carrying Retry-After must drive the wait, not the tiny default backoff.
// Regresses the bug where resp was nilled before backoff read the header.
func TestRetryRoundTripperHonorsRetryAfter(t *testing.T) {
	attempts := 0
	base := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		attempts++
		if attempts == 1 {
			h := http.Header{}
			h.Set("Retry-After", "1")
			return &http.Response{
				StatusCode: http.StatusTooManyRequests,
				Header:     h,
				Body:       io.NopCloser(strings.NewReader("slow down")),
			}, nil
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader("ok")),
		}, nil
	})

	rt := RetryRoundTripper{
		Base:           base,
		Max:            3,
		WaitMin:        time.Millisecond, // default backoff would be ~1ms
		WaitMax:        30 * time.Second,
		RespectHeaders: true,
	}

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://example.com", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}

	start := time.Now()
	resp, err := rt.RoundTrip(req)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("round trip error: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 after retry, got %d", resp.StatusCode)
	}
	if elapsed < 900*time.Millisecond {
		t.Fatalf("expected Retry-After (~1s) to drive the wait, waited only %v", elapsed)
	}
}
