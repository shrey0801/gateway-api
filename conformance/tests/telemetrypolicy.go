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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	xv1alpha1 "sigs.k8s.io/gateway-api/apisx/v1alpha1"
	"sigs.k8s.io/gateway-api/conformance/utils/http"
	"sigs.k8s.io/gateway-api/conformance/utils/kubernetes"
	confsuite "sigs.k8s.io/gateway-api/conformance/utils/suite"
	"sigs.k8s.io/gateway-api/conformance/utils/tlog"
	"sigs.k8s.io/gateway-api/conformance/utils/tracing"
	"sigs.k8s.io/gateway-api/pkg/features"
)

func init() {
	ConformanceTests = append(ConformanceTests, TelemetryPolicy)
}

// telemetryPolicyTracedRequests is the number of traced requests each
// positive data-plane test sends after tracing has been confirmed live.
const telemetryPolicyTracedRequests = 10

var TelemetryPolicy = confsuite.ConformanceTest{
	ShortName:   "TelemetryPolicy",
	Description: "An XTelemetryPolicy with tracing enabled and a 100% sampling rate attached to a Gateway must cause the implementation to export a span for every request to an OTLP collector referenced by the provider backendRef",
	Features: []features.FeatureName{
		features.SupportGateway,
		features.SupportHTTPRoute,
		features.SupportTelemetryPolicy,
		features.SupportTelemetryPolicyTracing,
	},
	Manifests: []string{"tests/telemetrypolicy.yaml"},
	Parallel:  true,
	Test: func(t *testing.T, suite *confsuite.ConformanceTestSuite) {
		ns := confsuite.InfrastructureNamespace
		routeNN := types.NamespacedName{Name: "telemetrypolicy", Namespace: ns}
		gwNN := types.NamespacedName{Name: "gateway-telemetrypolicy", Namespace: ns}
		policyNN := types.NamespacedName{Name: "normative-test", Namespace: ns}
		sinkNN := types.NamespacedName{Name: tracing.SinkName, Namespace: ns}

		kubernetes.NamespacesMustBeReady(t, suite.Client, suite.TimeoutConfig, []string{ns})
		gwAddr := kubernetes.GatewayAndHTTPRoutesMustBeAccepted(t, suite.Client, suite.TimeoutConfig, suite.ControllerName, kubernetes.NewGatewayRef(gwNN), routeNN)
		kubernetes.HTTPRouteMustHaveResolvedRefsConditionsTrue(t, suite.Client, suite.TimeoutConfig, routeNN, gwNN)

		telemetryPolicyMustBeAcceptedAndResolved(t, suite, policyNN, gwNN)

		baseReq := http.ExpectedResponse{
			Namespace: ns,
			Request: http.Request{
				Host: "telemetrypolicy.example.com",
				Path: "/telemetrypolicy",
			},
			Response: http.Response{StatusCode: 200},
		}

		// Positive control: wait until the data plane is programmed and the
		// first span reaches the sink, so that the requests measured below
		// are not sent before the policy has taken effect.
		tracing.TracingMustBeLive(t, suite, gwAddr, sinkNN, baseReq)

		t.Run("Every request must result in an exported span", func(t *testing.T) {
			tokens := make([]string, 0, telemetryPolicyTracedRequests)
			for range telemetryPolicyTracedRequests {
				tokens = append(tokens, tracing.SendTracedRequest(t, suite, gwAddr, baseReq))
			}

			// The token is sent both in the mapped request header and in the
			// request path, and Matcher{Token} accepts either, so this test
			// does not depend on attribute support beyond what is needed to
			// correlate a span with its request.
			for _, token := range tokens {
				spans := tracing.SpansMustBeExported(t, suite, sinkNN, tracing.Matcher{Token: token})
				for i := range spans {
					if spans[i].Kind != "SERVER" {
						// Only informational: the API does not mandate a
						// span kind for the Gateway's span.
						tlog.Logf(t, "span %s for token %s has kind %q, expected SERVER", spans[i].SpanID, token, spans[i].Kind)
					}
				}
			}
		})
	},
}

// telemetryPolicyMustBeAcceptedAndResolved waits for the XTelemetryPolicy
// policyNN to report Accepted=True and ResolvedRefs=True for the Gateway
// ancestor gwNN.
func telemetryPolicyMustBeAcceptedAndResolved(t *testing.T, suite *confsuite.ConformanceTestSuite, policyNN, gwNN types.NamespacedName) {
	t.Helper()

	acceptedCond := metav1.Condition{
		Type:   string(gatewayv1.PolicyConditionAccepted),
		Status: metav1.ConditionTrue,
		Reason: string(gatewayv1.PolicyReasonAccepted),
	}
	resolvedRefsCond := metav1.Condition{
		Type:   string(xv1alpha1.TelemetryPolicyConditionResolvedRefs),
		Status: metav1.ConditionTrue,
		Reason: string(xv1alpha1.TelemetryPolicyReasonResolvedRefs),
	}
	kubernetes.TelemetryPolicyMustHaveCondition(t, suite.Client, suite.TimeoutConfig, policyNN, gwNN, acceptedCond)
	kubernetes.TelemetryPolicyMustHaveCondition(t, suite.Client, suite.TimeoutConfig, policyNN, gwNN, resolvedRefsCond)
}
