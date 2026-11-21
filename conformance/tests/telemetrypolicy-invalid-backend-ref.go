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
	"sigs.k8s.io/controller-runtime/pkg/client"

	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	xv1alpha1 "sigs.k8s.io/gateway-api/apisx/v1alpha1"
	"sigs.k8s.io/gateway-api/conformance/utils/kubernetes"
	confsuite "sigs.k8s.io/gateway-api/conformance/utils/suite"
	"sigs.k8s.io/gateway-api/pkg/features"
)

func init() {
	ConformanceTests = append(ConformanceTests, TelemetryPolicyInvalidBackendRef)
}

var TelemetryPolicyInvalidBackendRef = confsuite.ConformanceTest{
	ShortName:   "TelemetryPolicyInvalidBackendRef",
	Description: "An XTelemetryPolicy whose tracing provider backendRef cannot be resolved should have a ResolvedRefs status condition set to False with reason BackendNotFound, InvalidKind or RefNotPermitted as appropriate, and a cross-namespace backendRef should become resolved once a ReferenceGrant permits it.",
	Features: []features.FeatureName{
		features.SupportGateway,
		features.SupportHTTPRoute,
		features.SupportReferenceGrant,
		features.SupportTelemetryPolicy,
		features.SupportTelemetryPolicyTracing,
	},
	Manifests: []string{"tests/telemetrypolicy-invalid-backend-ref.yaml"},
	Parallel:  true,
	Test: func(t *testing.T, suite *confsuite.ConformanceTestSuite) {
		ns := confsuite.InfrastructureNamespace

		kubernetes.NamespacesMustBeReady(t, suite.Client, suite.TimeoutConfig, []string{ns, confsuite.AppBackendNamespace})

		// Each policy targets its own Gateway because only one XTelemetryPolicy
		// may apply to a given Gateway at a time.
		testCases := []struct {
			name   string
			reason gatewayv1.PolicyConditionReason
		}{
			{name: "backend-not-found", reason: xv1alpha1.TelemetryPolicyReasonBackendNotFound},
			{name: "invalid-kind", reason: xv1alpha1.TelemetryPolicyReasonInvalidKind},
			{name: "ref-not-permitted", reason: xv1alpha1.TelemetryPolicyReasonRefNotPermitted},
		}

		for _, tc := range testCases {
			policyNN := types.NamespacedName{Name: tc.name, Namespace: ns}
			gwNN := types.NamespacedName{Name: "gateway-telemetrypolicy-" + tc.name, Namespace: ns}
			routeNN := types.NamespacedName{Name: "telemetrypolicy-" + tc.name, Namespace: ns}

			t.Run("XTelemetryPolicy "+tc.name+" has a ResolvedRefs Condition with status False and Reason "+string(tc.reason), func(t *testing.T) {
				kubernetes.GatewayAndHTTPRoutesMustBeAccepted(t, suite.Client, suite.TimeoutConfig, suite.ControllerName, kubernetes.NewGatewayRef(gwNN), routeNN)

				// Whether an unresolved provider also makes the policy
				// un-Accepted is not specified by the API, so only
				// ResolvedRefs is asserted here.
				resolvedRefsCond := metav1.Condition{
					Type:   string(xv1alpha1.TelemetryPolicyConditionResolvedRefs),
					Status: metav1.ConditionFalse,
					Reason: string(tc.reason),
				}
				kubernetes.TelemetryPolicyMustHaveCondition(t, suite.Client, suite.TimeoutConfig, policyNN, gwNN, resolvedRefsCond)
			})
		}

		t.Run("XTelemetryPolicy ref-not-permitted has a ResolvedRefs Condition with status True once a ReferenceGrant permits the cross-namespace backendRef", func(t *testing.T) {
			policyNN := types.NamespacedName{Name: "ref-not-permitted", Namespace: ns}
			gwNN := types.NamespacedName{Name: "gateway-telemetrypolicy-ref-not-permitted", Namespace: ns}

			referenceGrant := &gatewayv1.ReferenceGrant{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "telemetrypolicy-ref-not-permitted",
					Namespace: confsuite.AppBackendNamespace,
				},
				Spec: gatewayv1.ReferenceGrantSpec{
					From: []gatewayv1.ReferenceGrantFrom{{
						Group:     gatewayv1.Group(xv1alpha1.GroupName),
						Kind:      gatewayv1.Kind("XTelemetryPolicy"),
						Namespace: gatewayv1.Namespace(ns),
					}},
					To: []gatewayv1.ReferenceGrantTo{{
						Group: "",
						Kind:  gatewayv1.Kind("Service"),
					}},
				},
			}
			suite.Applier.MustApplyObjectsWithCleanup(t, suite.Client, suite.TimeoutConfig, []client.Object{referenceGrant}, suite.CleanupTestResources)

			resolvedRefsCond := metav1.Condition{
				Type:   string(xv1alpha1.TelemetryPolicyConditionResolvedRefs),
				Status: metav1.ConditionTrue,
				Reason: string(xv1alpha1.TelemetryPolicyReasonResolvedRefs),
			}
			kubernetes.TelemetryPolicyMustHaveCondition(t, suite.Client, suite.TimeoutConfig, policyNN, gwNN, resolvedRefsCond)
		})
	},
}
