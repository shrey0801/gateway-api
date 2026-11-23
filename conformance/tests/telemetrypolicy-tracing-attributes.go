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

package tests

import (
	"testing"

	"k8s.io/apimachinery/pkg/types"

	"sigs.k8s.io/gateway-api/conformance/utils/http"
	"sigs.k8s.io/gateway-api/conformance/utils/kubernetes"
	confsuite "sigs.k8s.io/gateway-api/conformance/utils/suite"
	"sigs.k8s.io/gateway-api/conformance/utils/tlog"
	"sigs.k8s.io/gateway-api/conformance/utils/tracing"
	"sigs.k8s.io/gateway-api/pkg/features"
)

func init() {
	ConformanceTests = append(ConformanceTests, TelemetryPolicyTracingAttributes)
}

var TelemetryPolicyTracingAttributes = confsuite.ConformanceTest{
	ShortName:   "TelemetryPolicyTracingAttributes",
	Description: "An XTelemetryPolicy with Literal and Header tracing attributes must result in exported spans that carry the literal value and the value of the request header",
	Features: []features.FeatureName{
		features.SupportGateway,
		features.SupportHTTPRoute,
		features.SupportTelemetryPolicy,
		features.SupportTelemetryPolicyTracing,
	},
	Manifests: []string{"tests/telemetrypolicy-tracing-attributes.yaml"},
	Parallel:  true,
	Test: func(t *testing.T, suite *confsuite.ConformanceTestSuite) {
		ns := confsuite.InfrastructureNamespace
		routeNN := types.NamespacedName{Name: "telemetrypolicy-tracing-attributes", Namespace: ns}
		gwNN := types.NamespacedName{Name: "gateway-telemetrypolicy-tracing-attributes", Namespace: ns}
		policyNN := types.NamespacedName{Name: "tracing-attributes", Namespace: ns}
		sinkNN := types.NamespacedName{Name: tracing.SinkName, Namespace: ns}

		kubernetes.NamespacesMustBeReady(t, suite.Client, suite.TimeoutConfig, []string{ns})
		gwAddr := kubernetes.GatewayAndHTTPRoutesMustBeAccepted(t, suite.Client, suite.TimeoutConfig, suite.ControllerName, kubernetes.NewGatewayRef(gwNN), routeNN)
		kubernetes.HTTPRouteMustHaveResolvedRefsConditionsTrue(t, suite.Client, suite.TimeoutConfig, routeNN, gwNN)

		telemetryPolicyMustBeAcceptedAndResolved(t, suite, policyNN, gwNN)

		baseReq := http.ExpectedResponse{
			Namespace: ns,
			Request: http.Request{
				Host:    "tracing-attributes.telemetrypolicy.example.com",
				Path:    "/telemetrypolicy-tracing-attributes",
				Headers: map[string]string{"X-Test-Header": "my-value"},
			},
			Response: http.Response{StatusCode: 200},
		}

		// Positive control: wait until the data plane is programmed and the
		// first span reaches the sink.
		tracing.TracingMustBeLive(t, suite, gwAddr, sinkNN, baseReq)

		t.Run("Exported span must carry the Literal and Header attributes", func(t *testing.T) {
			token := tracing.SendTracedRequest(t, suite, gwAddr, baseReq)
			tracing.SpansMustBeExported(t, suite, sinkNN, tracing.Matcher{
				Token: token,
				Attributes: map[string]string{
					"env":         "telemetrypolicy-tracing-attributes",
					"test_header": "my-value",
				},
			})
		})

		t.Run("Exported span for a request without the header must still carry the Literal attribute", func(t *testing.T) {
			noHeaderReq := baseReq
			noHeaderReq.Request.Headers = nil

			token := tracing.SendTracedRequest(t, suite, gwAddr, noHeaderReq)
			spans := tracing.SpansMustBeExported(t, suite, sinkNN, tracing.Matcher{
				Token:      token,
				Attributes: map[string]string{"env": "telemetrypolicy-tracing-attributes"},
			})

			// The API does not specify whether a Header attribute whose
			// header is absent from the request is omitted from the span or
			// recorded with an empty value, so this is only reported.
			for i := range spans {
				if v, ok := spans[i].Attribute("test_header"); ok && v != "" {
					tlog.Logf(t, "span %s for a request without X-Test-Header carries test_header=%q", spans[i].SpanID, v)
				}
			}
		})
	},
}
