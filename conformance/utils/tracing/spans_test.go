/*
Copyright The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package tracing

import (
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	h "sigs.k8s.io/gateway-api/conformance/utils/http"
)

const sampleLine = `SPAN {"traceId":"4bf92f3577b34da6a3ce929d0e0e4736","spanId":"00f067aa0ba902b7","parentSpanId":"","name":"ingress","kind":"SERVER","startTimeUnixNano":1700000000000000000,"endTimeUnixNano":1700000000500000000,"status":"UNSET","attributes":{"http.request.method":"GET","url.path":"/telemetrypolicy/abc-123","gateway_api_conformance.token":"abc-123","env":"test"},"resource":{"service.name":"gw"},"scope":"envoy","transport":"grpc","exportHeaders":{"authorization":"Bearer x"}}`

func TestParseLogLine(t *testing.T) {
	tests := []struct {
		name string
		line string
		want *Span
		ok   bool
	}{
		{
			name: "span line",
			line: sampleLine,
			ok:   true,
			want: &Span{
				TraceID:           "4bf92f3577b34da6a3ce929d0e0e4736",
				SpanID:            "00f067aa0ba902b7",
				Name:              "ingress",
				Kind:              "SERVER",
				StartTimeUnixNano: 1700000000000000000,
				EndTimeUnixNano:   1700000000500000000,
				Status:            "UNSET",
				Attributes: map[string]string{
					"http.request.method":           "GET",
					"url.path":                      "/telemetrypolicy/abc-123",
					"gateway_api_conformance.token": "abc-123",
					"env":                           "test",
				},
				Resource:      map[string]string{"service.name": "gw"},
				Scope:         "envoy",
				Transport:     "grpc",
				ExportHeaders: map[string]string{"authorization": "Bearer x"},
			},
		},
		{
			name: "span line with surrounding whitespace",
			line: "  " + sampleLine + "\r",
			ok:   true,
		},
		{
			name: "minimal span line",
			line: `SPAN {"traceId":"ab","spanId":"cd","name":"n","kind":"SERVER"}`,
			ok:   true,
			want: &Span{TraceID: "ab", SpanID: "cd", Name: "n", Kind: "SERVER"},
		},
		{name: "empty line", line: ""},
		{name: "unrelated log line", line: "otel-sink listening on :4317 (grpc), :4318 (http), :8080 (query)"},
		{name: "prefix without payload", line: "SPAN "},
		{name: "prefix with invalid json", line: `SPAN {"traceId":`},
		{name: "prefix not at start", line: `2026-01-01T00:00:00Z SPAN {"traceId":"ab"}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := ParseLogLine(tc.line)
			require.Equal(t, tc.ok, ok)
			if !tc.ok {
				assert.Nil(t, got)
				return
			}
			require.NotNil(t, got)
			if tc.want != nil {
				assert.Equal(t, tc.want, got)
			}
		})
	}
}

func TestSpanAttribute(t *testing.T) {
	span, ok := ParseLogLine(sampleLine)
	require.True(t, ok)

	v, ok := span.Attribute("env")
	assert.True(t, ok)
	assert.Equal(t, "test", v)

	_, ok = span.Attribute("missing")
	assert.False(t, ok)

	var empty Span
	_, ok = empty.Attribute("env")
	assert.False(t, ok)
}

func TestMatcher(t *testing.T) {
	span, ok := ParseLogLine(sampleLine)
	require.True(t, ok)

	tests := []struct {
		name string
		m    Matcher
		want bool
	}{
		{name: "empty matcher matches everything", m: Matcher{}, want: true},
		{name: "token attribute", m: Matcher{Token: "abc-123"}, want: true},
		{name: "token not present", m: Matcher{Token: "zzz"}, want: false},
		{name: "trace id", m: Matcher{TraceID: "4bf92f3577b34da6a3ce929d0e0e4736"}, want: true},
		{name: "trace id case insensitive", m: Matcher{TraceID: "4BF92F3577B34DA6A3CE929D0E0E4736"}, want: true},
		{name: "wrong trace id", m: Matcher{TraceID: "00000000000000000000000000000001"}, want: false},
		{name: "name", m: Matcher{Name: "ingress"}, want: true},
		{name: "wrong name", m: Matcher{Name: "egress"}, want: false},
		{name: "attributes subset", m: Matcher{Attributes: map[string]string{"env": "test", "http.request.method": "GET"}}, want: true},
		{name: "attribute wrong value", m: Matcher{Attributes: map[string]string{"env": "prod"}}, want: false},
		{name: "attribute missing", m: Matcher{Attributes: map[string]string{"nope": ""}}, want: false},
		{name: "resource", m: Matcher{Resource: map[string]string{"service.name": "gw"}}, want: true},
		{name: "resource wrong value", m: Matcher{Resource: map[string]string{"service.name": "other"}}, want: false},
		{name: "all fields", m: Matcher{Token: "abc-123", TraceID: "4bf92f3577b34da6a3ce929d0e0e4736", Name: "ingress", Attributes: map[string]string{"env": "test"}, Resource: map[string]string{"service.name": "gw"}}, want: true},
		{name: "all fields but one", m: Matcher{Token: "abc-123", TraceID: "4bf92f3577b34da6a3ce929d0e0e4736", Name: "ingress", Attributes: map[string]string{"env": "other"}}, want: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, tc.m.Matches(span), tc.m.String())
		})
	}

	assert.False(t, Matcher{}.Matches(nil))
}

func TestMatcherTokenFallbacks(t *testing.T) {
	const token = "0f2d0a2e-6f1e-4d1c-9d8b-3c2a1b0e9f8d"

	tests := []struct {
		name string
		span Span
		want bool
	}{
		{
			name: "token attribute exact match",
			span: Span{Attributes: map[string]string{TokenAttr: token}},
			want: true,
		},
		{
			name: "token attribute different token",
			span: Span{Attributes: map[string]string{TokenAttr: "other"}},
			want: false,
		},
		{
			name: "token in url path attribute",
			span: Span{Attributes: map[string]string{"url.path": "/telemetrypolicy-tracing-disabled/" + token}},
			want: true,
		},
		{
			name: "token in full url attribute",
			span: Span{Attributes: map[string]string{"http.url": "http://disabled.telemetrypolicy.example.com/telemetrypolicy-tracing-disabled/" + token + "?x=y"}},
			want: true,
		},
		{
			name: "token in span name",
			span: Span{Name: "GET /telemetrypolicy/" + token},
			want: true,
		},
		{
			name: "token only in resource is not a match",
			span: Span{Resource: map[string]string{"service.name": token}},
			want: false,
		},
		{
			name: "no attributes",
			span: Span{Name: "ingress"},
			want: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, Matcher{Token: token}.Matches(&tc.span))
		})
	}
}

func TestMatcherFilter(t *testing.T) {
	spans := []Span{
		{Name: "a", Attributes: map[string]string{TokenAttr: "t1"}},
		{Name: "b", Attributes: map[string]string{TokenAttr: "t2"}},
		{Name: "c", Attributes: map[string]string{TokenAttr: "t1", "env": "x"}},
	}
	got := Matcher{Token: "t1"}.Filter(spans)
	require.Len(t, got, 2)
	assert.Equal(t, "a", got[0].Name)
	assert.Equal(t, "c", got[1].Name)

	assert.Empty(t, Matcher{Token: "t3"}.Filter(spans))
	assert.Empty(t, Matcher{}.Filter(nil))
}

func TestMatcherString(t *testing.T) {
	m := Matcher{
		Token:      "tok",
		TraceID:    "tid",
		Name:       "n",
		Attributes: map[string]string{"b": "2", "a": "1"},
		Resource:   map[string]string{"service.name": "gw"},
	}
	assert.Equal(t, "{token=tok traceId=tid name=n attributes.a=1 attributes.b=2 resource.service.name=gw}", m.String())
	assert.Equal(t, "{}", Matcher{}.String())
}

func TestNewTraceParent(t *testing.T) {
	traceParentRe := regexp.MustCompile(`^00-([0-9a-f]{32})-([0-9a-f]{16})-(0[01])$`)

	seen := map[string]bool{}
	for _, sampled := range []bool{true, false} {
		for range 10 {
			header, traceID := NewTraceParent(sampled)

			m := traceParentRe.FindStringSubmatch(header)
			require.NotNil(t, m, "traceparent %q does not match the W3C format", header)
			assert.Equal(t, traceID, m[1], "returned trace id must be the one in the header")
			assert.NotEqual(t, "00000000000000000000000000000000", m[1], "trace id must not be all zeros")
			assert.NotEqual(t, "0000000000000000", m[2], "parent span id must not be all zeros")
			if sampled {
				assert.Equal(t, "01", m[3])
			} else {
				assert.Equal(t, "00", m[3])
			}

			assert.False(t, seen[traceID], "trace ids must be unique")
			seen[traceID] = true
		}
	}
}

func TestWithToken(t *testing.T) {
	const token = "tok"

	original := h.ExpectedResponse{
		Namespace: "ns",
		Request: h.Request{
			Host:    "example.com",
			Path:    "/telemetrypolicy",
			Headers: map[string]string{"X-Test-Header": "my-value"},
		},
		Response: h.Response{StatusCode: 200},
	}
	traced := withToken(original, token)

	assert.Equal(t, "/telemetrypolicy/tok", traced.Request.Path)
	assert.Equal(t, map[string]string{"X-Test-Header": "my-value", TokenHeader: token}, traced.Request.Headers)
	assert.Nil(t, traced.ExpectedRequest)
	assert.Equal(t, "ns", traced.Namespace)
	assert.Equal(t, 200, traced.Response.StatusCode)

	// The input must not be modified.
	assert.Equal(t, "/telemetrypolicy", original.Request.Path)
	assert.Equal(t, map[string]string{"X-Test-Header": "my-value"}, original.Request.Headers)

	t.Run("no headers and trailing slash", func(t *testing.T) {
		traced := withToken(h.ExpectedResponse{Request: h.Request{Path: "/telemetrypolicy/"}}, token)
		assert.Equal(t, "/telemetrypolicy/tok", traced.Request.Path)
		assert.Equal(t, map[string]string{TokenHeader: token}, traced.Request.Headers)
	})

	t.Run("explicit expected request", func(t *testing.T) {
		exp := h.ExpectedResponse{
			Request: h.Request{Path: "/p", Headers: map[string]string{"A": "1"}},
			ExpectedRequest: &h.ExpectedRequest{
				Request: h.Request{Path: "/p", Headers: map[string]string{"A": "1"}},
			},
		}
		traced := withToken(exp, token)
		require.NotNil(t, traced.ExpectedRequest)
		assert.Equal(t, "/p/tok", traced.ExpectedRequest.Path)
		assert.Equal(t, map[string]string{"A": "1", TokenHeader: token}, traced.ExpectedRequest.Headers)
		assert.Equal(t, "/p", exp.ExpectedRequest.Path)
		assert.Equal(t, map[string]string{"A": "1"}, exp.ExpectedRequest.Headers)
	})
}
