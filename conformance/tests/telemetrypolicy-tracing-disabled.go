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
	"k8s.io/client-go/util/retry"

	xv1alpha1 "sigs.k8s.io/gateway-api/apisx/v1alpha1"
	"sigs.k8s.io/gateway-api/conformance/utils/http"
	"sigs.k8s.io/gateway-api/conformance/utils/kubernetes"
	confsuite "sigs.k8s.io/gateway-api/conformance/utils/suite"
	"sigs.k8s.io/gateway-api/conformance/utils/tracing"
	"sigs.k8s.io/gateway-api/pkg/features"
)

func init() {
	ConformanceTests = append(ConformanceTests, TelemetryPolicyTracingDisabled)
}

var TelemetryPolicyTracingDisabled = confsuite.ConformanceTest{
	ShortName:   "TelemetryPolicyTracingDisabled",
	Description: "Changing the tracing mode of an XTelemetryPolicy from Enabled to Disabled must stop the implementation from exporting spans for requests to the targeted Gateway while traffic keeps flowing",
	Features: []features.FeatureName{
		features.SupportGateway,
		features.SupportHTTPRoute,
		features.SupportTelemetryPolicy,
		features.SupportTelemetryPolicyTracing,
	},
	Manifests: []string{"tests/telemetrypolicy-tracing-disabled.yaml"},
	Parallel:  true,
	Slow:      true,
	Test: func(t *testing.T, suite *confsuite.ConformanceTestSuite) {
		ns := confsuite.InfrastructureNamespace
		routeNN := types.NamespacedName{Name: "telemetrypolicy-tracing-disabled", Namespace: ns}
		gwNN := types.NamespacedName{Name: "gateway-telemetrypolicy-tracing-disabled", Namespace: ns}
		policyNN := types.NamespacedName{Name: "tracing-disabled", Namespace: ns}
		sinkNN := types.NamespacedName{Name: tracing.SinkName, Namespace: ns}

		kubernetes.NamespacesMustBeReady(t, suite.Client, suite.TimeoutConfig, []string{ns})
		gwAddr := kubernetes.GatewayAndHTTPRoutesMustBeAccepted(t, suite.Client, suite.TimeoutConfig, suite.ControllerName, kubernetes.NewGatewayRef(gwNN), routeNN)
		kubernetes.HTTPRouteMustHaveResolvedRefsConditionsTrue(t, suite.Client, suite.TimeoutConfig, routeNN, gwNN)

		telemetryPolicyMustBeAcceptedAndResolved(t, suite, policyNN, gwNN)

		baseReq := http.ExpectedResponse{
			Namespace: ns,
			Request: http.Request{
				Host: "tracing-disabled.telemetrypolicy.example.com",
				Path: "/telemetrypolicy-tracing-disabled",
			},
			Response: http.Response{StatusCode: 200},
		}

		// Positive control: spans must flow while tracing is Enabled,
		// otherwise the absence assertion below would be meaningless.
		tracing.TracingMustBeLive(t, suite, gwAddr, sinkNN, baseReq)

		t.Run("Spans must not be exported once tracing is Disabled", func(t *testing.T) {
			// provider, samplingRate and attributes are only valid when
			// mode is Enabled, so disabling tracing means replacing the
			// whole tracing block rather than flipping mode alone.
			err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
				policy := &xv1alpha1.XTelemetryPolicy{}
				if err := suite.Client.Get(t.Context(), policyNN, policy); err != nil {
					return err
				}
				policy.Spec.Tracing = xv1alpha1.TracingConfig{Mode: xv1alpha1.TracingModeDisabled}
				return suite.Client.Update(t.Context(), policy)
			})
			require.NoError(t, err, "error updating the XTelemetryPolicy to disable tracing")

			// Wait for the implementation to observe the new generation
			// before probing, so that a span exported for a request that
			// raced with the update is not mistaken for a violation.
			kubernetes.TelemetryPolicyMustHaveAcceptedConditionTrue(t, suite.Client, suite.TimeoutConfig, policyNN, gwNN)
			policy := &xv1alpha1.XTelemetryPolicy{}
			require.NoError(t, suite.Client.Get(t.Context(), policyNN, policy), "error getting the XTelemetryPolicy")
			kubernetes.TelemetryPolicyMustHaveLatestConditions(t, policy)

			// The token header is no longer mapped to a span attribute, so
			// a span exported in violation of the Disabled mode can only be
			// correlated through the token in the request path (which
			// Matcher{Token} also looks for in any attribute value and in
			// the span name). Traffic must keep succeeding throughout.
			tracing.SpansMustNotBeExported(t, suite, gwAddr, sinkNN, baseReq)
		})
	},
}
