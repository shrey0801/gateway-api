//go:build experimental
// +build experimental

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

package main

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	xgatewayv1alpha1 "sigs.k8s.io/gateway-api/apisx/v1alpha1"
)

func validTracingProvider() *xgatewayv1alpha1.TracingProvider {
	return &xgatewayv1alpha1.TracingProvider{
		BackendRef: gatewayv1.BackendObjectReference{
			Name: "otel-collector",
			Port: new(gatewayv1.PortNumber(4317)),
		},
	}
}

func validTargetRefs() []gatewayv1.LocalObjectReference {
	return []gatewayv1.LocalObjectReference{
		{
			Group: gatewayv1.GroupName,
			Kind:  "Gateway",
			Name:  "my-gateway",
		},
	}
}

func validEnabledTracing() xgatewayv1alpha1.TracingConfig {
	return xgatewayv1alpha1.TracingConfig{
		Mode:     xgatewayv1alpha1.TracingModeEnabled,
		Provider: validTracingProvider(),
	}
}

func TestXTelemetryPolicySpec(t *testing.T) {
	manyTargetRefs := make([]gatewayv1.LocalObjectReference, 0, 17)
	for i := range 17 {
		manyTargetRefs = append(manyTargetRefs, gatewayv1.LocalObjectReference{
			Group: gatewayv1.GroupName,
			Kind:  "Gateway",
			Name:  gatewayv1.ObjectName(fmt.Sprintf("my-gateway-%d", i)),
		})
	}

	tests := []struct {
		name       string
		spec       xgatewayv1alpha1.TelemetryPolicySpec
		wantErrors []string
	}{
		{
			name: "spec with targetRefs and tracing is accepted",
			spec: xgatewayv1alpha1.TelemetryPolicySpec{
				TargetRefs: validTargetRefs(),
				Tracing:    validEnabledTracing(),
			},
			wantErrors: []string{},
		},
		{
			name: "spec without tracing is rejected",
			spec: xgatewayv1alpha1.TelemetryPolicySpec{
				TargetRefs: validTargetRefs(),
			},
			wantErrors: []string{"at least one of the fields in [tracing] must be set"},
		},
		{
			name: "spec without targetRefs is rejected",
			spec: xgatewayv1alpha1.TelemetryPolicySpec{
				Tracing: validEnabledTracing(),
			},
			wantErrors: []string{"spec.targetRefs"},
		},
		{
			name: "spec with empty targetRefs is rejected",
			spec: xgatewayv1alpha1.TelemetryPolicySpec{
				TargetRefs: []gatewayv1.LocalObjectReference{},
				Tracing:    validEnabledTracing(),
			},
			wantErrors: []string{"spec.targetRefs in body should have at least 1 items"},
		},
		{
			name: "spec with more than 16 targetRefs is rejected",
			spec: xgatewayv1alpha1.TelemetryPolicySpec{
				TargetRefs: manyTargetRefs,
				Tracing:    validEnabledTracing(),
			},
			wantErrors: []string{"spec.targetRefs: Too many: 17: must have at most 16 items"},
		},
		{
			name: "targetRef with empty kind is rejected",
			spec: xgatewayv1alpha1.TelemetryPolicySpec{
				TargetRefs: []gatewayv1.LocalObjectReference{
					{
						Group: gatewayv1.GroupName,
						Kind:  "",
						Name:  "my-gateway",
					},
				},
				Tracing: validEnabledTracing(),
			},
			wantErrors: []string{"spec.targetRefs[0].kind in body should be at least 1 chars long"},
		},
		{
			name: "targetRef with empty name is rejected",
			spec: xgatewayv1alpha1.TelemetryPolicySpec{
				TargetRefs: []gatewayv1.LocalObjectReference{
					{
						Group: gatewayv1.GroupName,
						Kind:  "Gateway",
						Name:  "",
					},
				},
				Tracing: validEnabledTracing(),
			},
			wantErrors: []string{"spec.targetRefs[0].name in body should be at least 1 chars long"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			policy := &xgatewayv1alpha1.XTelemetryPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf("foo-%v", time.Now().UnixNano()),
					Namespace: metav1.NamespaceDefault,
				},
				Spec: tc.spec,
			}
			validateXTelemetryPolicy(t, policy, tc.wantErrors)
		})
	}
}

func TestXTelemetryPolicySamplingRate(t *testing.T) {
	tests := []struct {
		name         string
		samplingRate *gatewayv1.Fraction
		wantErrors   []string
	}{
		{
			name: "samplingRate with numerator equal to denominator is accepted",
			samplingRate: &gatewayv1.Fraction{
				Numerator:   100,
				Denominator: new(int32(100)),
			},
			wantErrors: []string{},
		},
		{
			name: "samplingRate with explicit non-default denominator is accepted",
			samplingRate: &gatewayv1.Fraction{
				Numerator:   1,
				Denominator: new(int32(1000)),
			},
			wantErrors: []string{},
		},
		{
			name: "samplingRate with numerator greater than denominator is rejected",
			samplingRate: &gatewayv1.Fraction{
				Numerator:   150,
				Denominator: new(int32(100)),
			},
			wantErrors: []string{"numerator must be less than or equal to denominator"},
		},
		{
			name: "samplingRate with numerator greater than default denominator is rejected",
			samplingRate: &gatewayv1.Fraction{
				Numerator: 101,
			},
			wantErrors: []string{"numerator must be less than or equal to denominator"},
		},
		{
			name: "samplingRate with negative numerator is rejected",
			samplingRate: &gatewayv1.Fraction{
				Numerator: -1,
			},
			wantErrors: []string{"spec.tracing.samplingRate.numerator in body should be greater than or equal to 0"},
		},
		{
			name: "samplingRate with zero denominator is rejected",
			samplingRate: &gatewayv1.Fraction{
				Numerator:   0,
				Denominator: new(int32(0)),
			},
			wantErrors: []string{"spec.tracing.samplingRate.denominator in body should be greater than or equal to 1"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tracing := validEnabledTracing()
			tracing.SamplingRate = tc.samplingRate
			policy := &xgatewayv1alpha1.XTelemetryPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf("foo-%v", time.Now().UnixNano()),
					Namespace: metav1.NamespaceDefault,
				},
				Spec: xgatewayv1alpha1.TelemetryPolicySpec{
					TargetRefs: validTargetRefs(),
					Tracing:    tracing,
				},
			}
			validateXTelemetryPolicy(t, policy, tc.wantErrors)
		})
	}
}

func TestXTelemetryPolicyAttributes(t *testing.T) {
	manyAttributes := make([]xgatewayv1alpha1.Attribute, 0, 17)
	for i := range 17 {
		manyAttributes = append(manyAttributes, xgatewayv1alpha1.Attribute{
			Name:         xgatewayv1alpha1.AttributeName(fmt.Sprintf("attr-%d", i)),
			SourceType:   xgatewayv1alpha1.AttributeSourceLiteral,
			LiteralValue: "value",
		})
	}

	tests := []struct {
		name       string
		attributes []xgatewayv1alpha1.Attribute
		wantErrors []string
	}{
		{
			name: "attribute with sourceType Header and headerName is accepted",
			attributes: []xgatewayv1alpha1.Attribute{
				{
					Name:       "user.agent",
					SourceType: xgatewayv1alpha1.AttributeSourceHeader,
					HeaderName: "User-Agent",
				},
			},
			wantErrors: []string{},
		},
		{
			name: "attribute with sourceType Literal and literalValue is accepted",
			attributes: []xgatewayv1alpha1.Attribute{
				{
					Name:         "env",
					SourceType:   xgatewayv1alpha1.AttributeSourceLiteral,
					LiteralValue: "prod",
				},
			},
			wantErrors: []string{},
		},
		{
			name: "attribute with sourceType Attribute and attributeKey is accepted",
			attributes: []xgatewayv1alpha1.Attribute{
				{
					Name:         "http.method",
					SourceType:   xgatewayv1alpha1.AttributeSourceAttribute,
					AttributeKey: "http.request.method",
				},
			},
			wantErrors: []string{},
		},
		{
			name: "attribute with sourceType Header without headerName is rejected",
			attributes: []xgatewayv1alpha1.Attribute{
				{
					Name:       "user.agent",
					SourceType: xgatewayv1alpha1.AttributeSourceHeader,
				},
			},
			wantErrors: []string{"headerName is required when sourceType is Header, and must be empty otherwise"},
		},
		{
			name: "attribute with sourceType Literal without literalValue is rejected",
			attributes: []xgatewayv1alpha1.Attribute{
				{
					Name:       "env",
					SourceType: xgatewayv1alpha1.AttributeSourceLiteral,
				},
			},
			wantErrors: []string{"literalValue is required when sourceType is Literal, and must be empty otherwise"},
		},
		{
			name: "attribute with sourceType Attribute without attributeKey is rejected",
			attributes: []xgatewayv1alpha1.Attribute{
				{
					Name:       "http.method",
					SourceType: xgatewayv1alpha1.AttributeSourceAttribute,
				},
			},
			wantErrors: []string{"attributeKey is required when sourceType is Attribute, and must be empty otherwise"},
		},
		{
			name: "attribute with sourceType Literal and headerName is rejected",
			attributes: []xgatewayv1alpha1.Attribute{
				{
					Name:         "env",
					SourceType:   xgatewayv1alpha1.AttributeSourceLiteral,
					LiteralValue: "prod",
					HeaderName:   "User-Agent",
				},
			},
			wantErrors: []string{"headerName is required when sourceType is Header, and must be empty otherwise"},
		},
		{
			name: "attribute with sourceType Header and attributeKey is rejected",
			attributes: []xgatewayv1alpha1.Attribute{
				{
					Name:         "user.agent",
					SourceType:   xgatewayv1alpha1.AttributeSourceHeader,
					HeaderName:   "User-Agent",
					AttributeKey: "http.request.method",
				},
			},
			wantErrors: []string{"attributeKey is required when sourceType is Attribute, and must be empty otherwise"},
		},
		{
			name: "attribute with sourceType Attribute and literalValue is rejected",
			attributes: []xgatewayv1alpha1.Attribute{
				{
					Name:         "http.method",
					SourceType:   xgatewayv1alpha1.AttributeSourceAttribute,
					AttributeKey: "http.request.method",
					LiteralValue: "GET",
				},
			},
			wantErrors: []string{"literalValue is required when sourceType is Literal, and must be empty otherwise"},
		},
		{
			name: "attribute with unknown sourceType is rejected",
			attributes: []xgatewayv1alpha1.Attribute{
				{
					Name:       "env",
					SourceType: "Unknown",
				},
			},
			wantErrors: []string{"spec.tracing.attributes[0].sourceType: Unsupported value: \"Unknown\""},
		},
		{
			name: "attribute with uppercase attributeKey is rejected",
			attributes: []xgatewayv1alpha1.Attribute{
				{
					Name:         "http.method",
					SourceType:   xgatewayv1alpha1.AttributeSourceAttribute,
					AttributeKey: "Http.Request.Method",
				},
			},
			wantErrors: []string{"spec.tracing.attributes[0].attributeKey in body should match '^[a-z0-9_.-]+$'"},
		},
		{
			name: "attribute with whitespace in name is rejected",
			attributes: []xgatewayv1alpha1.Attribute{
				{
					Name:         "my attribute",
					SourceType:   xgatewayv1alpha1.AttributeSourceLiteral,
					LiteralValue: "prod",
				},
			},
			wantErrors: []string{"spec.tracing.attributes[0].name in body should match '^[a-zA-Z0-9_.:/-]+$'"},
		},
		{
			name: "attribute with invalid headerName is rejected",
			attributes: []xgatewayv1alpha1.Attribute{
				{
					Name:       "user.agent",
					SourceType: xgatewayv1alpha1.AttributeSourceHeader,
					HeaderName: "User Agent",
				},
			},
			wantErrors: []string{"spec.tracing.attributes[0].headerName in body should match"},
		},
		{
			name: "attributes with duplicate names are rejected",
			attributes: []xgatewayv1alpha1.Attribute{
				{
					Name:         "env",
					SourceType:   xgatewayv1alpha1.AttributeSourceLiteral,
					LiteralValue: "prod",
				},
				{
					Name:       "env",
					SourceType: xgatewayv1alpha1.AttributeSourceHeader,
					HeaderName: "X-Env",
				},
			},
			wantErrors: []string{"spec.tracing.attributes[1]: Duplicate value"},
		},
		{
			name:       "more than 16 attributes are rejected",
			attributes: manyAttributes,
			wantErrors: []string{"spec.tracing.attributes: Too many: 17: must have at most 16 items"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tracing := validEnabledTracing()
			tracing.Attributes = tc.attributes
			policy := &xgatewayv1alpha1.XTelemetryPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf("foo-%v", time.Now().UnixNano()),
					Namespace: metav1.NamespaceDefault,
				},
				Spec: xgatewayv1alpha1.TelemetryPolicySpec{
					TargetRefs: validTargetRefs(),
					Tracing:    tracing,
				},
			}
			validateXTelemetryPolicy(t, policy, tc.wantErrors)
		})
	}
}

func TestXTelemetryPolicyServiceAndSpanName(t *testing.T) {
	tests := []struct {
		name        string
		serviceName string
		spanName    string
		wantErrors  []string
	}{
		{
			name:        "serviceName with allowed characters is accepted",
			serviceName: "my-gateway_service.v1:prod/us",
			wantErrors:  []string{},
		},
		{
			name:        "serviceName with wildcard is rejected",
			serviceName: "*.foo",
			wantErrors:  []string{"spec.tracing.serviceName in body should match '^[a-zA-Z0-9_.:/-]+$'"},
		},
		{
			name:        "serviceName with whitespace is rejected",
			serviceName: "my gateway",
			wantErrors:  []string{"spec.tracing.serviceName in body should match '^[a-zA-Z0-9_.:/-]+$'"},
		},
		{
			name:       "spanName with single spaces is accepted",
			spanName:   "HTTP GET /api",
			wantErrors: []string{},
		},
		{
			name:       "spanName with consecutive spaces is rejected",
			spanName:   "HTTP  GET",
			wantErrors: []string{"spec.tracing.spanName in body should match '^[a-zA-Z0-9_.:/-]+( [a-zA-Z0-9_.:/-]+)*$'"},
		},
		{
			name:       "spanName with leading space is rejected",
			spanName:   " HTTP GET",
			wantErrors: []string{"spec.tracing.spanName in body should match '^[a-zA-Z0-9_.:/-]+( [a-zA-Z0-9_.:/-]+)*$'"},
		},
		{
			name:       "spanName with trailing space is rejected",
			spanName:   "HTTP GET ",
			wantErrors: []string{"spec.tracing.spanName in body should match '^[a-zA-Z0-9_.:/-]+( [a-zA-Z0-9_.:/-]+)*$'"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tracing := validEnabledTracing()
			tracing.ServiceName = tc.serviceName
			tracing.SpanName = tc.spanName
			policy := &xgatewayv1alpha1.XTelemetryPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf("foo-%v", time.Now().UnixNano()),
					Namespace: metav1.NamespaceDefault,
				},
				Spec: xgatewayv1alpha1.TelemetryPolicySpec{
					TargetRefs: validTargetRefs(),
					Tracing:    tracing,
				},
			}
			validateXTelemetryPolicy(t, policy, tc.wantErrors)
		})
	}
}

func TestXTelemetryPolicyTracingProvider(t *testing.T) {
	manyHeaders := make([]gatewayv1.HTTPHeader, 0, 17)
	for i := range 17 {
		manyHeaders = append(manyHeaders, gatewayv1.HTTPHeader{
			Name:  gatewayv1.HTTPHeaderName(fmt.Sprintf("X-Header-%d", i)),
			Value: "value",
		})
	}

	tests := []struct {
		name       string
		provider   *xgatewayv1alpha1.TracingProvider
		wantErrors []string
	}{
		{
			name: "provider with Service backendRef and headers is accepted",
			provider: &xgatewayv1alpha1.TracingProvider{
				BackendRef: gatewayv1.BackendObjectReference{
					Name: "otel-collector",
					Port: new(gatewayv1.PortNumber(4317)),
				},
				Headers: []gatewayv1.HTTPHeader{
					{
						Name:  "Authorization",
						Value: "Bearer token",
					},
					{
						Name:  "X-Tenant",
						Value: "tenant-a",
					},
				},
			},
			wantErrors: []string{},
		},
		{
			name: "provider with Service backendRef without port is rejected",
			provider: &xgatewayv1alpha1.TracingProvider{
				BackendRef: gatewayv1.BackendObjectReference{
					Name: "otel-collector",
				},
			},
			wantErrors: []string{"Must have port for Service reference"},
		},
		{
			name: "provider with XBackend backendRef with port is rejected",
			provider: &xgatewayv1alpha1.TracingProvider{
				BackendRef: gatewayv1.BackendObjectReference{
					Group: new(gatewayv1.Group(xgatewayv1alpha1.GroupName)),
					Kind:  new(gatewayv1.Kind("XBackend")),
					Name:  "otel-collector",
					Port:  new(gatewayv1.PortNumber(4317)),
				},
			},
			wantErrors: []string{"Must not have port for Backend reference"},
		},
		{
			name: "provider with XBackend backendRef without port is accepted",
			provider: &xgatewayv1alpha1.TracingProvider{
				BackendRef: gatewayv1.BackendObjectReference{
					Group: new(gatewayv1.Group(xgatewayv1alpha1.GroupName)),
					Kind:  new(gatewayv1.Kind("XBackend")),
					Name:  "otel-collector",
				},
			},
			wantErrors: []string{},
		},
		{
			name: "provider with backendRef without name is rejected",
			provider: &xgatewayv1alpha1.TracingProvider{
				BackendRef: gatewayv1.BackendObjectReference{
					Port: new(gatewayv1.PortNumber(4317)),
				},
			},
			wantErrors: []string{"spec.tracing.provider.backendRef.name in body should be at least 1 chars long"},
		},
		{
			name: "provider with duplicate header names is rejected",
			provider: &xgatewayv1alpha1.TracingProvider{
				BackendRef: gatewayv1.BackendObjectReference{
					Name: "otel-collector",
					Port: new(gatewayv1.PortNumber(4317)),
				},
				Headers: []gatewayv1.HTTPHeader{
					{
						Name:  "X-Tenant",
						Value: "tenant-a",
					},
					{
						Name:  "X-Tenant",
						Value: "tenant-b",
					},
				},
			},
			wantErrors: []string{"spec.tracing.provider.headers[1]: Duplicate value"},
		},
		{
			name: "provider with more than 16 headers is rejected",
			provider: &xgatewayv1alpha1.TracingProvider{
				BackendRef: gatewayv1.BackendObjectReference{
					Name: "otel-collector",
					Port: new(gatewayv1.PortNumber(4317)),
				},
				Headers: manyHeaders,
			},
			wantErrors: []string{"spec.tracing.provider.headers: Too many: 17: must have at most 16 items"},
		},
		{
			name: "provider with invalid header name is rejected",
			provider: &xgatewayv1alpha1.TracingProvider{
				BackendRef: gatewayv1.BackendObjectReference{
					Name: "otel-collector",
					Port: new(gatewayv1.PortNumber(4317)),
				},
				Headers: []gatewayv1.HTTPHeader{
					{
						Name:  "X Tenant",
						Value: "tenant-a",
					},
				},
			},
			wantErrors: []string{"spec.tracing.provider.headers[0].name in body should match"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			policy := &xgatewayv1alpha1.XTelemetryPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf("foo-%v", time.Now().UnixNano()),
					Namespace: metav1.NamespaceDefault,
				},
				Spec: xgatewayv1alpha1.TelemetryPolicySpec{
					TargetRefs: validTargetRefs(),
					Tracing: xgatewayv1alpha1.TracingConfig{
						Mode:     xgatewayv1alpha1.TracingModeEnabled,
						Provider: tc.provider,
					},
				},
			}
			validateXTelemetryPolicy(t, policy, tc.wantErrors)
		})
	}
}

// TestXTelemetryPolicyDefaults verifies that defaults are applied by the
// API server, and that CEL rules are evaluated against the defaulted values.
// Unstructured objects are used so that empty objects such as `tracing: {}`
// (which the typed client omits via omitzero) can be sent explicitly.
func TestXTelemetryPolicyDefaults(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name       string
		tracing    map[string]any
		want       map[string]any
		wantErrors []string
	}{
		{
			name:    "empty tracing defaults mode to ImplementationDefault",
			tracing: map[string]any{},
			want: map[string]any{
				"spec.tracing.mode": "ImplementationDefault",
			},
			wantErrors: []string{},
		},
		{
			name: "tracing with provider but without mode is rejected because mode defaults to ImplementationDefault",
			tracing: map[string]any{
				"provider": map[string]any{
					"backendRef": map[string]any{
						"name": "otel-collector",
						"port": int64(4317),
					},
				},
			},
			wantErrors: []string{"provider is required when mode is Enabled, and must be empty otherwise"},
		},
		{
			name: "samplingRate without denominator defaults denominator to 100",
			tracing: map[string]any{
				"mode": "Enabled",
				"provider": map[string]any{
					"backendRef": map[string]any{
						"name": "otel-collector",
						"port": int64(4317),
					},
				},
				"samplingRate": map[string]any{
					"numerator": int64(5),
				},
			},
			want: map[string]any{
				"spec.tracing.samplingRate.denominator": int64(100),
			},
			wantErrors: []string{},
		},
		{
			name: "empty parentBasedSampling defaults mode to ImplementationDefault",
			tracing: map[string]any{
				"mode": "Enabled",
				"provider": map[string]any{
					"backendRef": map[string]any{
						"name": "otel-collector",
						"port": int64(4317),
					},
				},
				"parentBasedSampling": map[string]any{},
			},
			want: map[string]any{
				"spec.tracing.parentBasedSampling.mode": "ImplementationDefault",
			},
			wantErrors: []string{},
		},
		{
			name: "parentBasedSampling with samplingRate but without mode is rejected because mode defaults to ImplementationDefault",
			tracing: map[string]any{
				"mode": "Enabled",
				"provider": map[string]any{
					"backendRef": map[string]any{
						"name": "otel-collector",
						"port": int64(4317),
					},
				},
				"parentBasedSampling": map[string]any{
					"samplingRate": map[string]any{
						"numerator": int64(5),
					},
				},
			},
			wantErrors: []string{"samplingRate can only be set when mode is Enabled"},
		},
		{
			name: "provider backendRef defaults group and kind to Service",
			tracing: map[string]any{
				"mode": "Enabled",
				"provider": map[string]any{
					"backendRef": map[string]any{
						"name": "otel-collector",
						"port": int64(4317),
					},
				},
			},
			want: map[string]any{
				"spec.tracing.provider.backendRef.group": "",
				"spec.tracing.provider.backendRef.kind":  "Service",
			},
			wantErrors: []string{},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			name := fmt.Sprintf("foo-%v", time.Now().UnixNano())
			obj := &unstructured.Unstructured{Object: map[string]any{
				"apiVersion": fmt.Sprintf("%s/%s", xgatewayv1alpha1.GroupName, xgatewayv1alpha1.GroupVersion.Version),
				"kind":       "XTelemetryPolicy",
				"metadata": map[string]any{
					"name":      name,
					"namespace": metav1.NamespaceDefault,
				},
				"spec": map[string]any{
					"targetRefs": []any{map[string]any{
						"group": gatewayv1.GroupName,
						"kind":  "Gateway",
						"name":  "my-gateway",
					}},
					"tracing": tc.tracing,
				},
			}}

			err := k8sClient.Create(ctx, obj)
			if (len(tc.wantErrors) != 0) != (err != nil) {
				t.Fatalf("Unexpected response while creating XTelemetryPolicy %q; got err=\n%v\n;want error=%v", fmt.Sprintf("%v/%v", metav1.NamespaceDefault, name), err, tc.wantErrors)
			}

			var missingErrorStrings []string
			for _, wantError := range tc.wantErrors {
				if !celErrorStringMatches(err.Error(), wantError) {
					missingErrorStrings = append(missingErrorStrings, wantError)
				}
			}
			if len(missingErrorStrings) != 0 {
				t.Errorf("Unexpected response while creating XTelemetryPolicy %q; got err=\n%v\n;missing strings within error=%q", fmt.Sprintf("%v/%v", metav1.NamespaceDefault, name), err, missingErrorStrings)
			}

			for path, want := range tc.want {
				got, found, err := unstructured.NestedFieldNoCopy(obj.Object, strings.Split(path, ".")...)
				if err != nil {
					t.Fatalf("Failed to read %q from created XTelemetryPolicy: %v", path, err)
				}
				if !found {
					t.Errorf("Expected %q to be defaulted to %v, but it is not set", path, want)
					continue
				}
				if got != want {
					t.Errorf("Expected %q to be defaulted to %v, got %v", path, want, got)
				}
			}
		})
	}
}

func TestXTelemetryPolicyTracingMode(t *testing.T) {
	tests := []struct {
		name       string
		tracing    xgatewayv1alpha1.TracingConfig
		wantErrors []string
	}{
		{
			name: "mode Enabled with provider and sibling fields is accepted",
			tracing: xgatewayv1alpha1.TracingConfig{
				Mode:     xgatewayv1alpha1.TracingModeEnabled,
				Provider: validTracingProvider(),
				SamplingRate: &gatewayv1.Fraction{
					Numerator: 50,
				},
				ParentBasedSampling: xgatewayv1alpha1.ParentBasedSampling{
					Mode: xgatewayv1alpha1.ParentBasedSamplingModeEnabled,
					SamplingRate: &gatewayv1.Fraction{
						Numerator: 50,
					},
				},
				ServiceName: "my-gateway-service",
				SpanName:    "HTTP GET",
				Attributes: []xgatewayv1alpha1.Attribute{
					{
						Name:         "env",
						SourceType:   xgatewayv1alpha1.AttributeSourceLiteral,
						LiteralValue: "prod",
					},
				},
			},
			wantErrors: []string{},
		},
		{
			name: "mode Enabled with 0% samplingRate is accepted",
			tracing: xgatewayv1alpha1.TracingConfig{
				Mode:     xgatewayv1alpha1.TracingModeEnabled,
				Provider: validTracingProvider(),
				SamplingRate: &gatewayv1.Fraction{
					Numerator: 0,
				},
			},
			wantErrors: []string{},
		},
		{
			name: "mode Enabled without provider is rejected",
			tracing: xgatewayv1alpha1.TracingConfig{
				Mode: xgatewayv1alpha1.TracingModeEnabled,
			},
			wantErrors: []string{"provider is required when mode is Enabled, and must be empty otherwise"},
		},
		{
			name: "mode Disabled without sibling fields is accepted",
			tracing: xgatewayv1alpha1.TracingConfig{
				Mode: xgatewayv1alpha1.TracingModeDisabled,
			},
			wantErrors: []string{},
		},
		{
			name: "mode Disabled with provider is rejected",
			tracing: xgatewayv1alpha1.TracingConfig{
				Mode:     xgatewayv1alpha1.TracingModeDisabled,
				Provider: validTracingProvider(),
			},
			wantErrors: []string{"provider is required when mode is Enabled, and must be empty otherwise"},
		},
		{
			name: "mode Disabled with samplingRate is rejected",
			tracing: xgatewayv1alpha1.TracingConfig{
				Mode: xgatewayv1alpha1.TracingModeDisabled,
				SamplingRate: &gatewayv1.Fraction{
					Numerator: 10,
				},
			},
			wantErrors: []string{"samplingRate can only be set when mode is Enabled"},
		},
		{
			name: "mode Disabled with parentBasedSampling is rejected",
			tracing: xgatewayv1alpha1.TracingConfig{
				Mode: xgatewayv1alpha1.TracingModeDisabled,
				ParentBasedSampling: xgatewayv1alpha1.ParentBasedSampling{
					Mode: xgatewayv1alpha1.ParentBasedSamplingModeEnabled,
				},
			},
			wantErrors: []string{"parentBasedSampling can only be set when mode is Enabled"},
		},
		{
			name: "mode Disabled with serviceName is rejected",
			tracing: xgatewayv1alpha1.TracingConfig{
				Mode:        xgatewayv1alpha1.TracingModeDisabled,
				ServiceName: "my-service",
			},
			wantErrors: []string{"serviceName can only be set when mode is Enabled"},
		},
		{
			name: "mode Disabled with spanName is rejected",
			tracing: xgatewayv1alpha1.TracingConfig{
				Mode:     xgatewayv1alpha1.TracingModeDisabled,
				SpanName: "my-span",
			},
			wantErrors: []string{"spanName can only be set when mode is Enabled"},
		},
		{
			name: "mode Disabled with attributes is rejected",
			tracing: xgatewayv1alpha1.TracingConfig{
				Mode: xgatewayv1alpha1.TracingModeDisabled,
				Attributes: []xgatewayv1alpha1.Attribute{
					{
						Name:         "env",
						SourceType:   xgatewayv1alpha1.AttributeSourceLiteral,
						LiteralValue: "prod",
					},
				},
			},
			wantErrors: []string{"attributes can only be set when mode is Enabled"},
		},
		{
			name: "mode ImplementationDefault without sibling fields is accepted",
			tracing: xgatewayv1alpha1.TracingConfig{
				Mode: xgatewayv1alpha1.TracingModeImplementationDefault,
			},
			wantErrors: []string{},
		},
		{
			name: "mode ImplementationDefault with provider is rejected",
			tracing: xgatewayv1alpha1.TracingConfig{
				Mode:     xgatewayv1alpha1.TracingModeImplementationDefault,
				Provider: validTracingProvider(),
			},
			wantErrors: []string{"provider is required when mode is Enabled, and must be empty otherwise"},
		},
		{
			name: "mode ImplementationDefault with samplingRate is rejected",
			tracing: xgatewayv1alpha1.TracingConfig{
				Mode: xgatewayv1alpha1.TracingModeImplementationDefault,
				SamplingRate: &gatewayv1.Fraction{
					Numerator: 10,
				},
			},
			wantErrors: []string{"samplingRate can only be set when mode is Enabled"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			policy := &xgatewayv1alpha1.XTelemetryPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf("foo-%v", time.Now().UnixNano()),
					Namespace: metav1.NamespaceDefault,
				},
				Spec: xgatewayv1alpha1.TelemetryPolicySpec{
					TargetRefs: []gatewayv1.LocalObjectReference{
						{
							Group: gatewayv1.GroupName,
							Kind:  "Gateway",
							Name:  "my-gateway",
						},
					},
					Tracing: tc.tracing,
				},
			}
			validateXTelemetryPolicy(t, policy, tc.wantErrors)
		})
	}
}

func TestXTelemetryPolicyParentBasedSampling(t *testing.T) {
	tests := []struct {
		name                string
		parentBasedSampling xgatewayv1alpha1.ParentBasedSampling
		wantErrors          []string
	}{
		{
			name: "parentBasedSampling mode Enabled without samplingRate is accepted",
			parentBasedSampling: xgatewayv1alpha1.ParentBasedSampling{
				Mode: xgatewayv1alpha1.ParentBasedSamplingModeEnabled,
			},
			wantErrors: []string{},
		},
		{
			name: "parentBasedSampling mode Enabled with samplingRate is accepted",
			parentBasedSampling: xgatewayv1alpha1.ParentBasedSampling{
				Mode: xgatewayv1alpha1.ParentBasedSamplingModeEnabled,
				SamplingRate: &gatewayv1.Fraction{
					Numerator: 50,
				},
			},
			wantErrors: []string{},
		},
		{
			name: "parentBasedSampling mode Disabled without samplingRate is accepted",
			parentBasedSampling: xgatewayv1alpha1.ParentBasedSampling{
				Mode: xgatewayv1alpha1.ParentBasedSamplingModeDisabled,
			},
			wantErrors: []string{},
		},
		{
			name: "parentBasedSampling mode Disabled with samplingRate is rejected",
			parentBasedSampling: xgatewayv1alpha1.ParentBasedSampling{
				Mode: xgatewayv1alpha1.ParentBasedSamplingModeDisabled,
				SamplingRate: &gatewayv1.Fraction{
					Numerator: 50,
				},
			},
			wantErrors: []string{"samplingRate can only be set when mode is Enabled"},
		},
		{
			name: "parentBasedSampling mode ImplementationDefault without samplingRate is accepted",
			parentBasedSampling: xgatewayv1alpha1.ParentBasedSampling{
				Mode: xgatewayv1alpha1.ParentBasedSamplingModeImplementationDefault,
			},
			wantErrors: []string{},
		},
		{
			name: "parentBasedSampling mode ImplementationDefault with samplingRate is rejected",
			parentBasedSampling: xgatewayv1alpha1.ParentBasedSampling{
				Mode: xgatewayv1alpha1.ParentBasedSamplingModeImplementationDefault,
				SamplingRate: &gatewayv1.Fraction{
					Numerator: 50,
				},
			},
			wantErrors: []string{"samplingRate can only be set when mode is Enabled"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			policy := &xgatewayv1alpha1.XTelemetryPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf("foo-%v", time.Now().UnixNano()),
					Namespace: metav1.NamespaceDefault,
				},
				Spec: xgatewayv1alpha1.TelemetryPolicySpec{
					TargetRefs: []gatewayv1.LocalObjectReference{
						{
							Group: gatewayv1.GroupName,
							Kind:  "Gateway",
							Name:  "my-gateway",
						},
					},
					Tracing: xgatewayv1alpha1.TracingConfig{
						Mode:                xgatewayv1alpha1.TracingModeEnabled,
						Provider:            validTracingProvider(),
						ParentBasedSampling: tc.parentBasedSampling,
					},
				},
			}
			validateXTelemetryPolicy(t, policy, tc.wantErrors)
		})
	}
}

func validateXTelemetryPolicy(t *testing.T, policy *xgatewayv1alpha1.XTelemetryPolicy, wantErrors []string) {
	t.Helper()

	ctx := context.Background()
	err := k8sClient.Create(ctx, policy)

	if (len(wantErrors) != 0) != (err != nil) {
		t.Fatalf("Unexpected response while creating XTelemetryPolicy %q; got err=\n%v\n;want error=%v", fmt.Sprintf("%v/%v", policy.Namespace, policy.Name), err, wantErrors)
	}

	var missingErrorStrings []string
	for _, wantError := range wantErrors {
		if !celErrorStringMatches(err.Error(), wantError) {
			missingErrorStrings = append(missingErrorStrings, wantError)
		}
	}
	if len(missingErrorStrings) != 0 {
		t.Errorf("Unexpected response while creating XTelemetryPolicy %q; got err=\n%v\n;missing strings within error=%q", fmt.Sprintf("%v/%v", policy.Namespace, policy.Name), err, missingErrorStrings)
	}
}
