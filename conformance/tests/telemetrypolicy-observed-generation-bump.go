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
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"sigs.k8s.io/controller-runtime/pkg/client"

	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	xv1alpha1 "sigs.k8s.io/gateway-api/apisx/v1alpha1"
	"sigs.k8s.io/gateway-api/conformance/utils/kubernetes"
	confsuite "sigs.k8s.io/gateway-api/conformance/utils/suite"
	"sigs.k8s.io/gateway-api/pkg/features"
)

func init() {
	ConformanceTests = append(ConformanceTests, TelemetryPolicyObservedGenerationBump)
}

var TelemetryPolicyObservedGenerationBump = confsuite.ConformanceTest{
	ShortName:   "TelemetryPolicyObservedGenerationBump",
	Description: "An XTelemetryPolicy in the gateway-conformance-infra namespace should update the observedGeneration in all of its Status.Conditions after an update to the spec",
	Features: []features.FeatureName{
		features.SupportGateway,
		features.SupportHTTPRoute,
		features.SupportTelemetryPolicy,
	},
	Manifests: []string{"tests/telemetrypolicy-observed-generation-bump.yaml"},
	Parallel:  true,
	Test: func(t *testing.T, suite *confsuite.ConformanceTestSuite) {
		ns := confsuite.InfrastructureNamespace
		routeNN := types.NamespacedName{Name: "telemetrypolicy-observed-generation-bump", Namespace: ns}
		gwNN := types.NamespacedName{Name: "gateway-telemetrypolicy-observed-generation-bump", Namespace: ns}
		policyNN := types.NamespacedName{Name: "observed-generation-bump", Namespace: ns}

		kubernetes.NamespacesMustBeReady(t, suite.Client, suite.TimeoutConfig, []string{ns})
		kubernetes.GatewayAndHTTPRoutesMustBeAccepted(t, suite.Client, suite.TimeoutConfig, suite.ControllerName, kubernetes.NewGatewayRef(gwNN), routeNN)
		kubernetes.TelemetryPolicyMustHaveAcceptedConditionTrue(t, suite.Client, suite.TimeoutConfig, policyNN, gwNN)

		t.Run("observedGeneration should increment", func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), suite.TimeoutConfig.LatestObservedGenerationSet)
			defer cancel()

			original := &xv1alpha1.XTelemetryPolicy{}
			err := suite.Client.Get(ctx, policyNN, original)
			require.NoError(t, err, "error getting XTelemetryPolicy")

			// Sanity check
			kubernetes.TelemetryPolicyMustHaveLatestConditions(t, original)

			// Switching tracing.mode from ImplementationDefault to Disabled is
			// a spec change that stays valid without any sibling fields
			// (provider is only allowed, and required, when mode is Enabled).
			mutate := original.DeepCopy()
			mutate.Spec.Tracing.Mode = xv1alpha1.TracingModeDisabled
			err = suite.Client.Patch(ctx, mutate, client.MergeFrom(original))
			require.NoError(t, err, "error patching the XTelemetryPolicy")

			kubernetes.TelemetryPolicyMustHaveCondition(t, suite.Client, suite.TimeoutConfig, policyNN, gwNN, metav1.Condition{
				Type:   string(gatewayv1.PolicyConditionAccepted),
				Status: metav1.ConditionTrue,
				Reason: "", // any reason
			})

			updated := &xv1alpha1.XTelemetryPolicy{}
			err = suite.Client.Get(ctx, policyNN, updated)
			require.NoError(t, err, "error getting XTelemetryPolicy")

			// Sanity check
			kubernetes.TelemetryPolicyMustHaveLatestConditions(t, updated)

			require.NotEqual(t, original.Generation, updated.Generation, "generation should change after an update")
		})
	},
}
