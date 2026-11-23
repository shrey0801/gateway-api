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
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

const (
	// TraceParentHeader is the W3C Trace Context request header.
	TraceParentHeader = "traceparent"

	traceParentVersion      = "00"
	traceParentFlagsSampled = "01"
	traceParentFlagsNone    = "00"
)

// NewTraceParent returns a W3C Trace Context `traceparent` header value
// (https://www.w3.org/TR/trace-context/#traceparent-header) with a freshly
// generated trace id and parent span id, and the trace id it carries as
// lowercase hex. sampled controls the `sampled` trace flag.
func NewTraceParent(sampled bool) (header, traceID string) {
	traceID = randomHex(16)
	spanID := randomHex(8)
	flags := traceParentFlagsNone
	if sampled {
		flags = traceParentFlagsSampled
	}
	return fmt.Sprintf("%s-%s-%s-%s", traceParentVersion, traceID, spanID, flags), traceID
}

// randomHex returns n random bytes as lowercase hex. It never returns an
// all-zero value, which W3C Trace Context treats as invalid.
func randomHex(n int) string {
	b := make([]byte, n)
	for {
		if _, err := rand.Read(b); err != nil {
			panic(fmt.Sprintf("crypto/rand failed: %v", err))
		}
		for _, v := range b {
			if v != 0 {
				return hex.EncodeToString(b)
			}
		}
	}
}
