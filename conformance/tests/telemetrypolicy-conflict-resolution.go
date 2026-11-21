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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	xv1alpha1 "sigs.k8s.io/gateway-api/apisx/v1alpha1"
	h "sigs.k8s.io/gateway-api/conformance/utils/http"
	"sigs.k8s.io/gateway-api/conformance/utils/kubernetes"
	confsuite "sigs.k8s.io/gateway-api/conformance/utils/suite"
	"sigs.k8s.io/gateway-api/pkg/features"
)

func init() {
	ConformanceTests = append(ConformanceTests, TelemetryPolicyConflictResolution)
}

var TelemetryPolicyConflictResolution = confsuite.ConformanceTest{
	ShortName:   "TelemetryPolicyConflictResolution",
	Description: "Verifies that when multiple XTelemetryPolicies target the same Gateway, only the oldest policy is accepted while conflicting policies are rejected with reason Conflicted, and traffic continues to route successfully.",
	Features: []features.FeatureName{
		features.SupportGateway,
		features.SupportHTTPRoute,
		features.SupportTelemetryPolicy,
	},
	Manifests: []string{"tests/telemetrypolicy-conflict-resolution.yaml"},
	Parallel:  true,
	Test: func(t *testing.T, suite *confsuite.ConformanceTestSuite) {
		ns := confsuite.InfrastructureNamespace
		routeNN := types.NamespacedName{Name: "telemetrypolicy-conflict-resolution", Namespace: ns}
		gwNN := types.NamespacedName{Name: "gateway-telemetrypolicy-conflict-resolution", Namespace: ns}
		firstPolicyNN := types.NamespacedName{Name: "conflicted-1", Namespace: ns}
		secondPolicyNN := types.NamespacedName{Name: "conflicted-2", Namespace: ns}

		kubernetes.NamespacesMustBeReady(t, suite.Client, suite.TimeoutConfig, []string{ns})
		gwAddr := kubernetes.GatewayAndRoutesMustBeAccepted(t, suite.Client, suite.TimeoutConfig, suite.ControllerName, kubernetes.NewGatewayRef(gwNN), &gatewayv1.HTTPRoute{}, false, routeNN)
		kubernetes.HTTPRouteMustHaveResolvedRefsConditionsTrue(t, suite.Client, suite.TimeoutConfig, routeNN, gwNN)

		acceptedCond := metav1.Condition{
			Type:   string(gatewayv1.PolicyConditionAccepted),
			Status: metav1.ConditionTrue,
			Reason: string(gatewayv1.PolicyReasonAccepted),
		}
		conflictedCond := metav1.Condition{
			Type:   string(gatewayv1.PolicyConditionAccepted),
			Status: metav1.ConditionFalse,
			Reason: string(gatewayv1.PolicyReasonConflicted),
		}

		// Precedence between conflicting policies is decided by creation
		// timestamp, which only has second granularity. Wait for the first
		// policy to be accepted before creating the second one so that the
		// ordering is deterministic.
		t.Run("First XTelemetryPolicy should be accepted", func(t *testing.T) {
			kubernetes.TelemetryPolicyMustHaveCondition(t, suite.Client, suite.TimeoutConfig, firstPolicyNN, gwNN, acceptedCond)
		})

		secondPolicy := &xv1alpha1.XTelemetryPolicy{
			ObjectMeta: metav1.ObjectMeta{
				Name:      secondPolicyNN.Name,
				Namespace: secondPolicyNN.Namespace,
			},
			Spec: xv1alpha1.TelemetryPolicySpec{
				TargetRefs: []gatewayv1.LocalObjectReference{{
					Group: gatewayv1.Group(gatewayv1.GroupName),
					Kind:  gatewayv1.Kind("Gateway"),
					Name:  gatewayv1.ObjectName(gwNN.Name),
				}},
				Tracing: xv1alpha1.TracingConfig{
					Mode: xv1alpha1.TracingModeImplementationDefault,
				},
			},
		}
		suite.Applier.MustApplyObjectsWithCleanup(t, suite.Client, suite.TimeoutConfig, []client.Object{secondPolicy}, suite.CleanupTestResources)

		t.Run("Second XTelemetryPolicy should have a false Accepted condition with reason Conflicted", func(t *testing.T) {
			// This is not specific to XTelemetryPolicy, it follows the conflict-resolution rules, as defined in GEP-713.
			kubernetes.TelemetryPolicyMustHaveCondition(t, suite.Client, suite.TimeoutConfig, secondPolicyNN, gwNN, conflictedCond)
		})

		t.Run("First XTelemetryPolicy should remain accepted", func(t *testing.T) {
			kubernetes.TelemetryPolicyMustHaveCondition(t, suite.Client, suite.TimeoutConfig, firstPolicyNN, gwNN, acceptedCond)
		})

		t.Run("HTTP request sent to the Gateway targeted by conflicting XTelemetryPolicies should succeed", func(t *testing.T) {
			h.MakeRequestAndExpectEventuallyConsistentResponse(t, suite.RoundTripper, suite.TimeoutConfig, gwAddr,
				h.ExpectedResponse{
					Namespace: ns,
					Request: h.Request{
						Host: "conflict-resolution.telemetrypolicy.example.com",
						Path: "/telemetrypolicy-conflict-resolution",
					},
					Response: h.Response{StatusCode: 200},
				})
		})

		t.Run("Second XTelemetryPolicy should be accepted once the first one is deleted", func(t *testing.T) {
			firstPolicy := &xv1alpha1.XTelemetryPolicy{}
			require.NoError(t, suite.Client.Get(t.Context(), firstPolicyNN, firstPolicy), "error fetching XTelemetryPolicy %s", firstPolicyNN)
			require.NoError(t, suite.Client.Delete(t.Context(), firstPolicy), "error deleting XTelemetryPolicy %s", firstPolicyNN)

			kubernetes.TelemetryPolicyMustHaveCondition(t, suite.Client, suite.TimeoutConfig, secondPolicyNN, gwNN, acceptedCond)
		})
	},
}
