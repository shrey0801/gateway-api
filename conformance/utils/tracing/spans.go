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

// Package tracing contains helpers for the TelemetryPolicy data-plane
// conformance tests. They send requests carrying a unique correlation token
// through a Gateway and then read the logs of the shared `otel-sink` OTLP
// collector deployed by conformance/base/manifests.yaml to assert whether a
// span for that request was (or was not) exported by the implementation.
package tracing

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"

	h "sigs.k8s.io/gateway-api/conformance/utils/http"
	"sigs.k8s.io/gateway-api/conformance/utils/kubernetes"
	confsuite "sigs.k8s.io/gateway-api/conformance/utils/suite"
	"sigs.k8s.io/gateway-api/conformance/utils/tlog"
)

const (
	// SinkName is the name of the Service, Deployment, container and `app`
	// label of the OTLP trace sink deployed by conformance/base/manifests.yaml.
	SinkName = "otel-sink"

	// TokenHeader is the request header carrying the per-request correlation
	// token. Policies under test map it to the TokenAttr span attribute with
	// an attribute of sourceType Header.
	TokenHeader = "X-Gateway-Api-Conformance-Trace-Token"

	// TokenAttr is the span attribute name the correlation token is mapped to.
	TokenAttr = "gateway_api_conformance.token"

	// LogPrefix prefixes every span line written by the sink to its stdout.
	LogPrefix = "SPAN "

	// traceAbsenceProbes is the number of consecutive "request sent, no span
	// observed within TraceAbsenceWindow" probes SpansMustNotBeExported
	// requires. It is deliberately a constant rather than
	// TimeoutConfig.RequiredConsecutiveSuccesses: users commonly raise that
	// setting well beyond 3, which would make TraceAbsenceTimeout unreachable.
	traceAbsenceProbes = 3

	// logPollInterval is how often the sink logs are re-read while waiting
	// for a span. Pod log reads are comparatively expensive, so this is
	// intentionally coarser than TimeoutConfig.DefaultPollInterval.
	logPollInterval = time.Second
)

// Span is one span as logged by the otel-sink image, one JSON object per
// line prefixed with LogPrefix. All OTLP AnyValues are stringified by the
// sink; ids are lowercase hex.
//
// This is a local copy of the schema documented in
// https://github.com/kubernetes-sigs/gateway-api-conformance-images/tree/main/otel-sink
// and must be kept in sync with it.
type Span struct {
	TraceID           string            `json:"traceId"`
	SpanID            string            `json:"spanId"`
	ParentSpanID      string            `json:"parentSpanId,omitempty"`
	Name              string            `json:"name"`
	Kind              string            `json:"kind"`
	StartTimeUnixNano uint64            `json:"startTimeUnixNano,omitempty"`
	EndTimeUnixNano   uint64            `json:"endTimeUnixNano,omitempty"`
	Status            string            `json:"status,omitempty"`
	Attributes        map[string]string `json:"attributes,omitempty"`
	Resource          map[string]string `json:"resource,omitempty"`
	Scope             string            `json:"scope,omitempty"`
	Transport         string            `json:"transport,omitempty"`
	ExportHeaders     map[string]string `json:"exportHeaders,omitempty"`
}

// Attribute returns the value of the span attribute with the given key.
func (s *Span) Attribute(key string) (string, bool) {
	v, ok := s.Attributes[key]
	return v, ok
}

// String returns a compact single-line representation for test output.
func (s *Span) String() string {
	b, err := json.Marshal(s)
	if err != nil {
		return fmt.Sprintf("%+v", *s)
	}
	return string(b)
}

// ParseLogLine parses one line of otel-sink output. It returns false for
// lines that are not span records (empty lines, startup messages, ...).
func ParseLogLine(line string) (*Span, bool) {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, LogPrefix) {
		return nil, false
	}
	span := &Span{}
	if err := json.Unmarshal([]byte(strings.TrimPrefix(line, LogPrefix)), span); err != nil {
		return nil, false
	}
	return span, true
}

// Matcher selects spans. All set fields must match (logical AND); unset
// fields are ignored.
type Matcher struct {
	// Token matches spans correlated with a request sent by
	// SendTracedRequest. A span matches when its TokenAttr attribute equals
	// Token or, as a fallback for policies that do not (or no longer) map the
	// token header, when any attribute value or the span name contains Token
	// (the token is also sent as the last path segment of the request).
	Token string
	// TraceID matches the span's trace id (lowercase hex).
	TraceID string
	// Name matches the span name exactly.
	Name string
	// Attributes must all be present on the span with exactly these
	// (stringified) values.
	Attributes map[string]string
	// Resource attributes must all be present with exactly these values.
	Resource map[string]string
}

// Matches reports whether the span satisfies every field set on the Matcher.
func (m Matcher) Matches(s *Span) bool {
	if s == nil {
		return false
	}
	if m.Token != "" && !matchesToken(s, m.Token) {
		return false
	}
	if m.TraceID != "" && !strings.EqualFold(s.TraceID, m.TraceID) {
		return false
	}
	if m.Name != "" && s.Name != m.Name {
		return false
	}
	for k, want := range m.Attributes {
		if got, ok := s.Attributes[k]; !ok || got != want {
			return false
		}
	}
	for k, want := range m.Resource {
		if got, ok := s.Resource[k]; !ok || got != want {
			return false
		}
	}
	return true
}

// String returns a human readable description of the Matcher.
func (m Matcher) String() string {
	var parts []string
	if m.Token != "" {
		parts = append(parts, "token="+m.Token)
	}
	if m.TraceID != "" {
		parts = append(parts, "traceId="+m.TraceID)
	}
	if m.Name != "" {
		parts = append(parts, "name="+m.Name)
	}
	for _, kv := range sortedKV(m.Attributes) {
		parts = append(parts, "attributes."+kv)
	}
	for _, kv := range sortedKV(m.Resource) {
		parts = append(parts, "resource."+kv)
	}
	return "{" + strings.Join(parts, " ") + "}"
}

func sortedKV(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k, v := range m {
		out = append(out, k+"="+v)
	}
	sort.Strings(out)
	return out
}

func matchesToken(s *Span, token string) bool {
	if v, ok := s.Attributes[TokenAttr]; ok && v == token {
		return true
	}
	if strings.Contains(s.Name, token) {
		return true
	}
	for _, v := range s.Attributes {
		if strings.Contains(v, token) {
			return true
		}
	}
	return false
}

// Filter returns the spans matching m.
func (m Matcher) Filter(spans []Span) []Span {
	var out []Span
	for i := range spans {
		if m.Matches(&spans[i]) {
			out = append(out, spans[i])
		}
	}
	return out
}

// SendTracedRequest sends exp through the Gateway at gwAddr exactly as
// MakeRequestAndExpectEventuallyConsistentResponse would, but with
// RequiredConsecutiveSuccesses forced to 1 so that (barring retries of
// failed attempts) a single request is made, and returns the correlation
// token it carried. The token is sent both in the TokenHeader request header
// and as the last path segment (`<exp.Request.Path>/<token>`) so that spans
// can be correlated through either a Header attribute or the recorded URL.
func SendTracedRequest(t *testing.T, suite *confsuite.ConformanceTestSuite, gwAddr string, exp h.ExpectedResponse) string {
	t.Helper()

	token := uuid.NewString()
	traced := withToken(exp, token)

	timeoutConfig := suite.TimeoutConfig
	timeoutConfig.RequiredConsecutiveSuccesses = 1

	h.MakeRequestAndExpectEventuallyConsistentResponse(t, suite.RoundTripper, timeoutConfig, gwAddr, traced)
	return token
}

// withToken returns a copy of exp carrying token in the TokenHeader header
// and appended to the request path. exp itself is not modified.
func withToken(exp h.ExpectedResponse, token string) h.ExpectedResponse {
	tokenPath := strings.TrimSuffix(exp.Request.Path, "/") + "/" + token

	headers := maps.Clone(exp.Request.Headers)
	if headers == nil {
		headers = map[string]string{}
	}
	headers[TokenHeader] = token
	exp.Request.Headers = headers
	exp.Request.Path = tokenPath

	if exp.ExpectedRequest != nil {
		expectedRequest := *exp.ExpectedRequest
		expectedRequest.Path = tokenPath
		if expectedRequest.Headers != nil {
			expectedRequest.Headers = maps.Clone(expectedRequest.Headers)
			expectedRequest.Headers[TokenHeader] = token
		}
		exp.ExpectedRequest = &expectedRequest
	}
	return exp
}

// ReadSpans returns the spans logged by the sink since the given time.
func ReadSpans(ctx context.Context, suite *confsuite.ConformanceTestSuite, sink types.NamespacedName, since time.Time) ([]Span, error) {
	lines, err := kubernetes.DumpEchoLogs(ctx, sink.Namespace, sink.Name, suite.Client, suite.Clientset, since)
	if err != nil {
		return nil, err
	}
	var spans []Span
	for _, line := range lines {
		if span, ok := ParseLogLine(line); ok {
			spans = append(spans, *span)
		}
	}
	return spans, nil
}

// TracingMustBeLive repeatedly sends traced requests built from exp until a
// span for one of them is observed at the sink, failing the test if that
// does not happen within TraceExportTimeout. It is used as a positive
// control before any assertion so that the first measured request is not
// sent before the implementation has programmed the policy.
//
// Worst case it sends one request per logPollInterval for TraceExportTimeout
// (about 60 requests with the defaults); each request may additionally block
// for up to MaxTimeToConsistency while the Gateway is not yet returning the
// expected response, so the overall bound is
// TraceExportTimeout + MaxTimeToConsistency.
func TracingMustBeLive(t *testing.T, suite *confsuite.ConformanceTestSuite, gwAddr string, sink types.NamespacedName, exp h.ExpectedResponse) {
	t.Helper()

	// Start the log window before the first request is sent; SinceTime has
	// one-second granularity, see ExpectMirroredRequest for the rationale.
	since := time.Now().Add(-suite.TimeoutConfig.MaxTimeToConsistency)
	var tokens []string

	waitErr := wait.PollUntilContextTimeout(context.Background(), logPollInterval, suite.TimeoutConfig.TraceExportTimeout, true, func(ctx context.Context) (bool, error) {
		tokens = append(tokens, SendTracedRequest(t, suite, gwAddr, exp))

		spans, err := ReadSpans(ctx, suite, sink, since)
		if err != nil {
			tlog.Logf(t, "Couldn't read %s logs: %v", sink, err)
			return false, nil
		}
		for _, token := range tokens {
			if matched := (Matcher{Token: token}).Filter(spans); len(matched) > 0 {
				tlog.Logf(t, "Tracing is live: span for token %s observed at %s after %d request(s): %s", token, sink, len(tokens), matched[0].String())
				return true, nil
			}
		}
		return false, nil
	})
	require.NoErrorf(t, waitErr, "no span was exported to %s for any of %d traced requests to %s%s within %v", sink, len(tokens), exp.Request.Host, exp.Request.Path, suite.TimeoutConfig.TraceExportTimeout)
}

// SpansMustBeExported polls the sink logs until at least one span matching m
// has been exported, failing the test if that does not happen within
// TraceExportTimeout. It returns the matching spans. The log window starts
// MaxTimeToConsistency before the call so that it covers the request(s) the
// caller just sent.
func SpansMustBeExported(t *testing.T, suite *confsuite.ConformanceTestSuite, sink types.NamespacedName, m Matcher) []Span {
	t.Helper()

	since := time.Now().Add(-suite.TimeoutConfig.MaxTimeToConsistency)

	var matched []Span
	var candidates []Span
	waitErr := wait.PollUntilContextTimeout(context.Background(), logPollInterval, suite.TimeoutConfig.TraceExportTimeout, true, func(ctx context.Context) (bool, error) {
		spans, err := ReadSpans(ctx, suite, sink, since)
		if err != nil {
			tlog.Logf(t, "Couldn't read %s logs: %v", sink, err)
			return false, nil
		}
		matched = m.Filter(spans)
		if len(matched) > 0 {
			return true, nil
		}
		// Remember the spans correlated with the request (if any) so that a
		// failure can show what the implementation exported instead.
		if m.Token != "" {
			candidates = (Matcher{Token: m.Token}).Filter(spans)
		}
		return false, nil
	})
	if waitErr != nil {
		for i := range candidates {
			tlog.Logf(t, "Span for token %s did not match %s: %s", m.Token, m, candidates[i].String())
		}
		tlog.Fatalf(t, "no span matching %s was exported to %s within %v (%d span(s) carried the token but did not match)", m, sink, suite.TimeoutConfig.TraceExportTimeout, len(candidates))
	}
	tlog.Logf(t, "Found %d span(s) matching %s at %s", len(matched), m, sink)
	return matched
}

// SpansMustNotBeExported asserts that requests built from exp no longer
// produce spans at the sink. It sends a traced request, waits
// TraceAbsenceWindow and checks that no span carries the request's token,
// and requires traceAbsenceProbes such clean probes in a row (a span
// observed for any probe resets the count) within TraceAbsenceTimeout. It
// must only be used after a positive control on the same Gateway (for
// example TracingMustBeLive before the policy was changed) and after the
// policy status reports the latest generation, otherwise an implementation
// that never exported anything would pass trivially.
func SpansMustNotBeExported(t *testing.T, suite *confsuite.ConformanceTestSuite, gwAddr string, sink types.NamespacedName, exp h.ExpectedResponse) {
	t.Helper()

	deadline := time.Now().Add(suite.TimeoutConfig.TraceAbsenceTimeout)
	clean := 0
	probes := 0
	for clean < traceAbsenceProbes {
		if time.Now().After(deadline) {
			tlog.Fatalf(t, "spans were still being exported to %s after %d probe(s) over %v (%d/%d consecutive clean probes)", sink, probes, suite.TimeoutConfig.TraceAbsenceTimeout, clean, traceAbsenceProbes)
		}
		probes++

		since := time.Now().Add(-suite.TimeoutConfig.MaxTimeToConsistency)
		token := SendTracedRequest(t, suite, gwAddr, exp)
		time.Sleep(suite.TimeoutConfig.TraceAbsenceWindow)

		ctx, cancel := context.WithTimeout(context.Background(), suite.TimeoutConfig.RequestTimeout)
		spans, err := ReadSpans(ctx, suite, sink, since)
		cancel()
		if err != nil {
			tlog.Logf(t, "Couldn't read %s logs: %v", sink, err)
			clean = 0
			continue
		}
		if matched := (Matcher{Token: token}).Filter(spans); len(matched) > 0 {
			tlog.Logf(t, "Probe %d: %d span(s) still exported for token %s, e.g. %s; resetting", probes, len(matched), token, matched[0].String())
			clean = 0
			continue
		}
		clean++
		tlog.Logf(t, "Probe %d: no span for token %s within %v (%d/%d clean)", probes, token, suite.TimeoutConfig.TraceAbsenceWindow, clean, traceAbsenceProbes)
	}
}
