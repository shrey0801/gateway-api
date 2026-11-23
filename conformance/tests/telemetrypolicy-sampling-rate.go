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

	"github.com/stretchr/testify/require"

	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	xv1alpha1 "sigs.k8s.io/gateway-api/apisx/v1alpha1"
	"sigs.k8s.io/gateway-api/conformance/utils/http"
	"sigs.k8s.io/gateway-api/conformance/utils/kubernetes"
	confsuite "sigs.k8s.io/gateway-api/conformance/utils/suite"
	"sigs.k8s.io/gateway-api/conformance/utils/tracing"
	"sigs.k8s.io/gateway-api/pkg/features"
)

func init() {
	ConformanceTests = append(ConformanceTests, TelemetryPolicySamplingRate)
}

var TelemetryPolicySamplingRate = confsuite.ConformanceTest{
	ShortName:   "TelemetryPolicySamplingRate",
	Description: "An XTelemetryPolicy with a 100% tracing sampling rate must result in a span for every request, and lowering the sampling rate to 0% must stop spans from being exported",
	Features: []features.FeatureName{
		features.SupportGateway,
		features.SupportHTTPRoute,
		features.SupportTelemetryPolicy,
		features.SupportTelemetryPolicyTracing,
	},
	Manifests: []string{"tests/telemetrypolicy-sampling-rate.yaml"},
	Parallel:  true,
	Slow:      true,
	Test: func(t *testing.T, suite *confsuite.ConformanceTestSuite) {
		ns := confsuite.InfrastructureNamespace
		routeNN := types.NamespacedName{Name: "telemetrypolicy-sampling-rate", Namespace: ns}
		gwNN := types.NamespacedName{Name: "gateway-telemetrypolicy-sampling-rate", Namespace: ns}
		policyNN := types.NamespacedName{Name: "sampling-rate", Namespace: ns}
		sinkNN := types.NamespacedName{Name: tracing.SinkName, Namespace: ns}

		kubernetes.NamespacesMustBeReady(t, suite.Client, suite.TimeoutConfig, []string{ns})
		gwAddr := kubernetes.GatewayAndHTTPRoutesMustBeAccepted(t, suite.Client, suite.TimeoutConfig, suite.ControllerName, kubernetes.NewGatewayRef(gwNN), routeNN)
		kubernetes.HTTPRouteMustHaveResolvedRefsConditionsTrue(t, suite.Client, suite.TimeoutConfig, routeNN, gwNN)

		telemetryPolicyMustBeAcceptedAndResolved(t, suite, policyNN, gwNN)

		baseReq := http.ExpectedResponse{
			Namespace: ns,
			Request: http.Request{
				Host: "sampling-rate.telemetrypolicy.example.com",
				Path: "/telemetrypolicy-sampling-rate",
			},
			Response: http.Response{StatusCode: 200},
		}

		// Positive control: wait until the data plane is programmed and the
		// first span reaches the sink.
		tracing.TracingMustBeLive(t, suite, gwAddr, sinkNN, baseReq)

		t.Run("Every request must result in an exported span with a 100% sampling rate", func(t *testing.T) {
			tokens := make([]string, 0, telemetryPolicyTracedRequests)
			for range telemetryPolicyTracedRequests {
				tokens = append(tokens, tracing.SendTracedRequest(t, suite, gwAddr, baseReq))
			}
			for _, token := range tokens {
				tracing.SpansMustBeExported(t, suite, sinkNN, tracing.Matcher{Token: token})
			}
		})

		t.Run("No request must result in an exported span with a 0% sampling rate", func(t *testing.T) {
			original := &xv1alpha1.XTelemetryPolicy{}
			require.NoError(t, suite.Client.Get(t.Context(), policyNN, original), "error getting the XTelemetryPolicy")

			mutate := original.DeepCopy()
			require.NotNil(t, mutate.Spec.Tracing.SamplingRate, "the fixture must set tracing.samplingRate")
			mutate.Spec.Tracing.SamplingRate.Numerator = 0
			require.NoError(t, suite.Client.Patch(t.Context(), mutate, client.MergeFrom(original)), "error patching the XTelemetryPolicy sampling rate")

			// Wait for the implementation to observe the new generation
			// before probing, so that a span exported for a request that
			// raced with the update is not mistaken for a violation.
			kubernetes.TelemetryPolicyMustHaveAcceptedConditionTrue(t, suite.Client, suite.TimeoutConfig, policyNN, gwNN)
			policy := &xv1alpha1.XTelemetryPolicy{}
			require.NoError(t, suite.Client.Get(t.Context(), policyNN, policy), "error getting the XTelemetryPolicy")
			kubernetes.TelemetryPolicyMustHaveLatestConditions(t, policy)

			// The token header is still mapped to a span attribute, so any
			// span exported in violation of the 0% sampling rate is
			// correlated through it. Traffic must keep succeeding throughout.
			tracing.SpansMustNotBeExported(t, suite, gwAddr, sinkNN, baseReq)
		})
	},
}
