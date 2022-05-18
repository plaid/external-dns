/*
Copyright 2017 The Kubernetes Authors.

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

package source

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	v1 "k8s.io/api/core/v1"
	extensionsv1beta1 "k8s.io/api/extensions/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes/fake"

	"sigs.k8s.io/external-dns/endpoint"
)

// Validates that ingressSource is a Source
var _ Source = &ingressSource{}

type LegacyIngressSuite struct {
	suite.Suite
	sc             Source
	fooWithTargets *extensionsv1beta1.Ingress
}

func (suite *LegacyIngressSuite) SetupTest() {
	fakeClient := fake.NewSimpleClientset()

	suite.fooWithTargets = (legacyFakeIngress{
		name:        "foo-with-targets",
		namespace:   "default",
		dnsnames:    []string{"foo"},
		ips:         []string{"8.8.8.8"},
		hostnames:   []string{"v1"},
		annotations: map[string]string{ALBDualstackAnnotationKey: ALBDualstackAnnotationValue},
	}).Ingress()
	_, err := fakeClient.ExtensionsV1beta1().Ingresses(suite.fooWithTargets.Namespace).Create(context.Background(), suite.fooWithTargets, metav1.CreateOptions{})
	suite.NoError(err, "should succeed")

	suite.sc, err = NewLegacyIngressSource(
		context.TODO(),
		fakeClient,
		"",
		"",
		"{{.Name}}",
		false,
		false,
		false,
		false,
		labels.Everything(),
	)
	suite.NoError(err, "should initialize ingress source")
}

func (suite *LegacyIngressSuite) TestResourceLabelIsSet() {
	endpoints, _ := suite.sc.Endpoints(context.Background())
	for _, ep := range endpoints {
		suite.Equal("ingress/default/foo-with-targets", ep.Labels[endpoint.ResourceLabelKey], "should set correct resource label")
	}
}

func (suite *LegacyIngressSuite) TestDualstackLabelIsSet() {
	endpoints, _ := suite.sc.Endpoints(context.Background())
	for _, ep := range endpoints {
		suite.Equal("true", ep.Labels[endpoint.DualstackLabelKey], "should set dualstack label to true")
	}
}

func TestLegacyIngress(t *testing.T) {
	t.Parallel()

	suite.Run(t, new(IngressSuite))
	t.Run("endpointsFromIngress", testEndpointsFromIngress)
	t.Run("endpointsFromIngressHostnameSourceAnnotation", testEndpointsFromIngressHostnameSourceAnnotation)
	t.Run("Endpoints", testIngressEndpoints)
}

func TestNewLegacyIngressSource(t *testing.T) {
	t.Parallel()

	for _, ti := range []struct {
		title                    string
		annotationFilter         string
		fqdnTemplate             string
		combineFQDNAndAnnotation bool
		expectError              bool
	}{
		{
			title:        "invalid template",
			expectError:  true,
			fqdnTemplate: "{{.Name",
		},
		{
			title:       "valid empty template",
			expectError: false,
		},
		{
			title:        "valid template",
			expectError:  false,
			fqdnTemplate: "{{.Name}}-{{.Namespace}}.ext-dns.test.com",
		},
		{
			title:        "valid template",
			expectError:  false,
			fqdnTemplate: "{{.Name}}-{{.Namespace}}.ext-dns.test.com, {{.Name}}-{{.Namespace}}.ext-dna.test.com",
		},
		{
			title:                    "valid template",
			expectError:              false,
			fqdnTemplate:             "{{.Name}}-{{.Namespace}}.ext-dns.test.com, {{.Name}}-{{.Namespace}}.ext-dna.test.com",
			combineFQDNAndAnnotation: true,
		},
		{
			title:            "non-empty annotation filter label",
			expectError:      false,
			annotationFilter: "kubernetes.io/ingress.class=nginx",
		},
	} {
		ti := ti
		t.Run(ti.title, func(t *testing.T) {
			t.Parallel()

			_, err := NewLegacyIngressSource(
				context.TODO(),
				fake.NewSimpleClientset(),
				"",
				ti.annotationFilter,
				ti.fqdnTemplate,
				ti.combineFQDNAndAnnotation,
				false,
				false,
				false,
				labels.Everything(),
			)
			if ti.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func testEndpointsFromLegacyIngress(t *testing.T) {
	t.Parallel()

	for _, ti := range []struct {
		title                    string
		ingress                  fakeIngress
		ignoreHostnameAnnotation bool
		ignoreIngressTLSSpec     bool
		ignoreIngressRulesSpec   bool
		expected                 []*endpoint.Endpoint
	}{
		{
			title: "one rule.host one lb.hostname",
			ingress: fakeIngress{
				dnsnames:  []string{"foo.bar"}, // Kubernetes requires removal of trailing dot
				hostnames: []string{"lb.com"},  // Kubernetes omits the trailing dot
			},
			expected: []*endpoint.Endpoint{
				{
					DNSName: "foo.bar",
					Targets: endpoint.Targets{"lb.com"},
				},
			},
		},
		{
			title: "one rule.host one lb.IP",
			ingress: fakeIngress{
				dnsnames: []string{"foo.bar"},
				ips:      []string{"8.8.8.8"},
			},
			expected: []*endpoint.Endpoint{
				{
					DNSName: "foo.bar",
					Targets: endpoint.Targets{"8.8.8.8"},
				},
			},
		},
		{
			title: "one rule.host two lb.IP and two lb.Hostname",
			ingress: fakeIngress{
				dnsnames:  []string{"foo.bar"},
				ips:       []string{"8.8.8.8", "127.0.0.1"},
				hostnames: []string{"elb.com", "alb.com"},
			},
			expected: []*endpoint.Endpoint{
				{
					DNSName: "foo.bar",
					Targets: endpoint.Targets{"8.8.8.8", "127.0.0.1"},
				},
				{
					DNSName: "foo.bar",
					Targets: endpoint.Targets{"elb.com", "alb.com"},
				},
			},
		},
		{
			title: "no rule.host",
			ingress: fakeIngress{
				ips:       []string{"8.8.8.8", "127.0.0.1"},
				hostnames: []string{"elb.com", "alb.com"},
			},
			expected: []*endpoint.Endpoint{},
		},
		{
			title: "one empty rule.host",
			ingress: fakeIngress{
				dnsnames:  []string{""},
				ips:       []string{"8.8.8.8", "127.0.0.1"},
				hostnames: []string{"elb.com", "alb.com"},
			},
			expected: []*endpoint.Endpoint{},
		},
		{
			title: "no targets",
			ingress: fakeIngress{
				dnsnames: []string{""},
			},
			expected: []*endpoint.Endpoint{},
		},
		{
			title: "ignore rules with one rule.host one lb.hostname",
			ingress: fakeIngress{
				dnsnames:  []string{"test"},   // Kubernetes requires removal of trailing dot
				hostnames: []string{"lb.com"}, // Kubernetes omits the trailing dot
			},
			expected:               []*endpoint.Endpoint{},
			ignoreIngressRulesSpec: true,
		},
	} {
		t.Run(ti.title, func(t *testing.T) {
			realIngress := ti.ingress.Ingress()
			validateEndpoints(t, endpointsFromIngress(realIngress, ti.ignoreHostnameAnnotation, ti.ignoreIngressTLSSpec, ti.ignoreIngressRulesSpec), ti.expected)
		})
	}
}

func testEndpointsFromLegacyIngressHostnameSourceAnnotation(t *testing.T) {
	// Host names and host name annotation provided, with various values of the ingress-hostname-source annotation
	for _, ti := range []struct {
		title    string
		ingress  legacyFakeIngress
		expected []*endpoint.Endpoint
	}{
		{
			title: "No ingress-hostname-source annotation, one rule.host, one annotation host",
			ingress: legacyFakeIngress{
				dnsnames:    []string{"foo.bar"},
				annotations: map[string]string{hostnameAnnotationKey: "foo.baz"},
				hostnames:   []string{"lb.com"},
			},
			expected: []*endpoint.Endpoint{
				{
					DNSName: "foo.bar",
					Targets: endpoint.Targets{"lb.com"},
				},
				{
					DNSName: "foo.baz",
					Targets: endpoint.Targets{"lb.com"},
				},
			},
		},
		{
			title: "No ingress-hostname-source annotation, one rule.host",
			ingress: legacyFakeIngress{
				dnsnames:  []string{"foo.bar"},
				hostnames: []string{"lb.com"},
			},
			expected: []*endpoint.Endpoint{
				{
					DNSName: "foo.bar",
					Targets: endpoint.Targets{"lb.com"},
				},
			},
		},
		{
			title: "No ingress-hostname-source annotation, one rule.host, one annotation host",
			ingress: legacyFakeIngress{
				dnsnames:    []string{"foo.bar"},
				annotations: map[string]string{hostnameAnnotationKey: "foo.baz"},
				hostnames:   []string{"lb.com"},
			},
			expected: []*endpoint.Endpoint{
				{
					DNSName: "foo.bar",
					Targets: endpoint.Targets{"lb.com"},
				},
				{
					DNSName: "foo.baz",
					Targets: endpoint.Targets{"lb.com"},
				},
			},
		},
		{
			title: "Ingress-hostname-source=defined-hosts-only, one rule.host, one annotation host",
			ingress: legacyFakeIngress{
				dnsnames:    []string{"foo.bar"},
				annotations: map[string]string{hostnameAnnotationKey: "foo.baz", ingressHostnameSourceKey: "defined-hosts-only"},
				hostnames:   []string{"lb.com"},
			},
			expected: []*endpoint.Endpoint{
				{
					DNSName: "foo.bar",
					Targets: endpoint.Targets{"lb.com"},
				},
			},
		},
		{
			title: "Ingress-hostname-source=annotation-only, one rule.host, one annotation host",
			ingress: legacyFakeIngress{
				dnsnames:    []string{"foo.bar"},
				annotations: map[string]string{hostnameAnnotationKey: "foo.baz", ingressHostnameSourceKey: "annotation-only"},
				hostnames:   []string{"lb.com"},
			},
			expected: []*endpoint.Endpoint{
				{
					DNSName: "foo.baz",
					Targets: endpoint.Targets{"lb.com"},
				},
			},
		},
	} {
		t.Run(ti.title, func(t *testing.T) {
			realIngress := ti.ingress.Ingress()
			validateEndpoints(t, endpointsFromLegacyIngress(realIngress, false, false, false), ti.expected)
		})
	}
}

func testLegacyIngressEndpoints(t *testing.T) {
	t.Parallel()

	namespace := "testing"
	for _, ti := range []struct {
		title                    string
		targetNamespace          string
		annotationFilter         string
		ingressItems             []legacyFakeIngress
		expected                 []*endpoint.Endpoint
		expectError              bool
		fqdnTemplate             string
		combineFQDNAndAnnotation bool
		ignoreHostnameAnnotation bool
		ignoreIngressTLSSpec     bool
		ignoreIngressRulesSpec   bool
		ingressLabelSelector     labels.Selector
	}{
		{
			title:           "no ingress",
			targetNamespace: "",
		},
		{
			title:           "two simple ingresses",
			targetNamespace: "",
			ingressItems: []legacyFakeIngress{
				{
					name:      "fake1",
					namespace: namespace,
					dnsnames:  []string{"example.org"},
					ips:       []string{"8.8.8.8"},
				},
				{
					name:      "fake2",
					namespace: namespace,
					dnsnames:  []string{"new.org"},
					hostnames: []string{"lb.com"},
				},
			},
			expected: []*endpoint.Endpoint{
				{
					DNSName: "example.org",
					Targets: endpoint.Targets{"8.8.8.8"},
				},
				{
					DNSName: "new.org",
					Targets: endpoint.Targets{"lb.com"},
				},
			},
		},
		{
			title:                  "ignore rules",
			targetNamespace:        "",
			ignoreIngressRulesSpec: true,
			ingressItems: []legacyFakeIngress{
				{
					name:      "fake1",
					namespace: namespace,
					dnsnames:  []string{"example.org"},
					ips:       []string{"8.8.8.8"},
				},
				{
					name:      "fake2",
					namespace: namespace,
					dnsnames:  []string{"new.org"},
					hostnames: []string{"lb.com"},
				},
			},
			expected: []*endpoint.Endpoint{},
		},
		{
			title:           "two simple ingresses on different namespaces",
			targetNamespace: "",
			ingressItems: []legacyFakeIngress{
				{
					name:      "fake1",
					namespace: "testing1",
					dnsnames:  []string{"example.org"},
					ips:       []string{"8.8.8.8"},
				},
				{
					name:      "fake2",
					namespace: "testing2",
					dnsnames:  []string{"new.org"},
					hostnames: []string{"lb.com"},
				},
			},
			expected: []*endpoint.Endpoint{
				{
					DNSName: "example.org",
					Targets: endpoint.Targets{"8.8.8.8"},
				},
				{
					DNSName: "new.org",
					Targets: endpoint.Targets{"lb.com"},
				},
			},
		},
		{
			title:           "two simple ingresses on different namespaces with target namespace",
			targetNamespace: "testing1",
			ingressItems: []legacyFakeIngress{
				{
					name:      "fake1",
					namespace: "testing1",
					dnsnames:  []string{"example.org"},
					ips:       []string{"8.8.8.8"},
				},
				{
					name:      "fake2",
					namespace: "testing2",
					dnsnames:  []string{"new.org"},
					hostnames: []string{"lb.com"},
				},
			},
			expected: []*endpoint.Endpoint{
				{
					DNSName: "example.org",
					Targets: endpoint.Targets{"8.8.8.8"},
				},
			},
		},
		{
			title:            "valid matching annotation filter expression",
			targetNamespace:  "",
			annotationFilter: "kubernetes.io/ingress.class in (alb, nginx)",
			ingressItems: []legacyFakeIngress{
				{
					name:      "fake1",
					namespace: namespace,
					annotations: map[string]string{
						"kubernetes.io/ingress.class": "nginx",
					},
					dnsnames: []string{"example.org"},
					ips:      []string{"8.8.8.8"},
				},
			},
			expected: []*endpoint.Endpoint{
				{
					DNSName: "example.org",
					Targets: endpoint.Targets{"8.8.8.8"},
				},
			},
		},
		{
			title:            "valid non-matching annotation filter expression",
			targetNamespace:  "",
			annotationFilter: "kubernetes.io/ingress.class in (alb, nginx)",
			ingressItems: []legacyFakeIngress{
				{
					name:      "fake1",
					namespace: namespace,
					annotations: map[string]string{
						"kubernetes.io/ingress.class": "tectonic",
					},
					dnsnames: []string{"example.org"},
					ips:      []string{"8.8.8.8"},
				},
			},
			expected: []*endpoint.Endpoint{},
		},
		{
			title:            "invalid annotation filter expression",
			targetNamespace:  "",
			annotationFilter: "kubernetes.io/ingress.name in (a b)",
			ingressItems: []legacyFakeIngress{
				{
					name:      "fake1",
					namespace: namespace,
					annotations: map[string]string{
						"kubernetes.io/ingress.class": "alb",
					},
					dnsnames: []string{"example.org"},
					ips:      []string{"8.8.8.8"},
				},
			},
			expected:    []*endpoint.Endpoint{},
			expectError: true,
		},
		{
			title:            "valid matching annotation filter label",
			targetNamespace:  "",
			annotationFilter: "kubernetes.io/ingress.class=nginx",
			ingressItems: []legacyFakeIngress{
				{
					name:      "fake1",
					namespace: namespace,
					annotations: map[string]string{
						"kubernetes.io/ingress.class": "nginx",
					},
					dnsnames: []string{"example.org"},
					ips:      []string{"8.8.8.8"},
				},
			},
			expected: []*endpoint.Endpoint{
				{
					DNSName: "example.org",
					Targets: endpoint.Targets{"8.8.8.8"},
				},
			},
		},
		{
			title:            "valid non-matching annotation filter label",
			targetNamespace:  "",
			annotationFilter: "kubernetes.io/ingress.class=nginx",
			ingressItems: []legacyFakeIngress{
				{
					name:      "fake1",
					namespace: namespace,
					annotations: map[string]string{
						"kubernetes.io/ingress.class": "alb",
					},
					dnsnames: []string{"example.org"},
					ips:      []string{"8.8.8.8"},
				},
			},
			expected: []*endpoint.Endpoint{},
		},
		{
			title:           "our controller type is dns-controller",
			targetNamespace: "",
			ingressItems: []legacyFakeIngress{
				{
					name:      "fake1",
					namespace: namespace,
					annotations: map[string]string{
						controllerAnnotationKey: controllerAnnotationValue,
					},
					dnsnames: []string{"example.org"},
					ips:      []string{"8.8.8.8"},
				},
			},
			expected: []*endpoint.Endpoint{
				{
					DNSName: "example.org",
					Targets: endpoint.Targets{"8.8.8.8"},
				},
			},
		},
		{
			title:           "different controller types are ignored",
			targetNamespace: "",
			ingressItems: []legacyFakeIngress{
				{
					name:      "fake1",
					namespace: namespace,
					annotations: map[string]string{
						controllerAnnotationKey: "some-other-tool",
					},
					dnsnames: []string{"example.org"},
					ips:      []string{"8.8.8.8"},
				},
			},
			expected: []*endpoint.Endpoint{},
		},
		{
			title:           "template for ingress if host is missing",
			targetNamespace: "",
			ingressItems: []legacyFakeIngress{
				{
					name:      "fake1",
					namespace: namespace,
					annotations: map[string]string{
						controllerAnnotationKey: controllerAnnotationValue,
					},
					dnsnames:  []string{},
					ips:       []string{"8.8.8.8"},
					hostnames: []string{"elb.com"},
				},
			},
			expected: []*endpoint.Endpoint{
				{
					DNSName: "fake1.ext-dns.test.com",
					Targets: endpoint.Targets{"8.8.8.8"},
				},
				{
					DNSName: "fake1.ext-dns.test.com",
					Targets: endpoint.Targets{"elb.com"},
				},
			},
			fqdnTemplate: "{{.Name}}.ext-dns.test.com",
		},
		{
			title:           "another controller annotation skipped even with template",
			targetNamespace: "",
			ingressItems: []legacyFakeIngress{
				{
					name:      "fake1",
					namespace: namespace,
					annotations: map[string]string{
						controllerAnnotationKey: "other-controller",
					},
					dnsnames: []string{},
					ips:      []string{"8.8.8.8"},
				},
			},
			expected:     []*endpoint.Endpoint{},
			fqdnTemplate: "{{.Name}}.ext-dns.test.com",
		},
		{
			title:           "multiple FQDN template hostnames",
			targetNamespace: "",
			ingressItems: []legacyFakeIngress{
				{
					name:        "fake1",
					namespace:   namespace,
					annotations: map[string]string{},
					dnsnames:    []string{},
					ips:         []string{"8.8.8.8"},
				},
			},
			expected: []*endpoint.Endpoint{
				{
					DNSName:    "fake1.ext-dns.test.com",
					Targets:    endpoint.Targets{"8.8.8.8"},
					RecordType: endpoint.RecordTypeA,
				},
				{
					DNSName:    "fake1.ext-dna.test.com",
					Targets:    endpoint.Targets{"8.8.8.8"},
					RecordType: endpoint.RecordTypeA,
				},
			},
			fqdnTemplate: "{{.Name}}.ext-dns.test.com, {{.Name}}.ext-dna.test.com",
		},
		{
			title:           "multiple FQDN template hostnames",
			targetNamespace: "",
			ingressItems: []legacyFakeIngress{
				{
					name:        "fake1",
					namespace:   namespace,
					annotations: map[string]string{},
					dnsnames:    []string{},
					ips:         []string{"8.8.8.8"},
				},
				{
					name:      "fake2",
					namespace: namespace,
					annotations: map[string]string{
						targetAnnotationKey: "ingress-target.com",
					},
					dnsnames: []string{"example.org"},
					ips:      []string{},
				},
			},
			expected: []*endpoint.Endpoint{
				{
					DNSName:    "fake1.ext-dns.test.com",
					Targets:    endpoint.Targets{"8.8.8.8"},
					RecordType: endpoint.RecordTypeA,
				},
				{
					DNSName:    "fake1.ext-dna.test.com",
					Targets:    endpoint.Targets{"8.8.8.8"},
					RecordType: endpoint.RecordTypeA,
				},
				{
					DNSName:    "example.org",
					Targets:    endpoint.Targets{"ingress-target.com"},
					RecordType: endpoint.RecordTypeCNAME,
				},
				{
					DNSName:    "fake2.ext-dns.test.com",
					Targets:    endpoint.Targets{"ingress-target.com"},
					RecordType: endpoint.RecordTypeCNAME,
				},
				{
					DNSName:    "fake2.ext-dna.test.com",
					Targets:    endpoint.Targets{"ingress-target.com"},
					RecordType: endpoint.RecordTypeCNAME,
				},
			},
			fqdnTemplate:             "{{.Name}}.ext-dns.test.com, {{.Name}}.ext-dna.test.com",
			combineFQDNAndAnnotation: true,
		},
		{
			title:           "ingress rules with annotation",
			targetNamespace: "",
			ingressItems: []legacyFakeIngress{
				{
					name:      "fake1",
					namespace: namespace,
					annotations: map[string]string{
						targetAnnotationKey: "ingress-target.com",
					},
					dnsnames: []string{"example.org"},
					ips:      []string{},
				},
				{
					name:      "fake2",
					namespace: namespace,
					annotations: map[string]string{
						targetAnnotationKey: "ingress-target.com",
					},
					dnsnames: []string{"example2.org"},
					ips:      []string{"8.8.8.8"},
				},
				{
					name:      "fake3",
					namespace: namespace,
					annotations: map[string]string{
						targetAnnotationKey: "1.2.3.4",
					},
					dnsnames: []string{"example3.org"},
					ips:      []string{},
				},
			},
			expected: []*endpoint.Endpoint{
				{
					DNSName:    "example.org",
					Targets:    endpoint.Targets{"ingress-target.com"},
					RecordType: endpoint.RecordTypeCNAME,
				},
				{
					DNSName:    "example2.org",
					Targets:    endpoint.Targets{"ingress-target.com"},
					RecordType: endpoint.RecordTypeCNAME,
				},
				{
					DNSName:    "example3.org",
					Targets:    endpoint.Targets{"1.2.3.4"},
					RecordType: endpoint.RecordTypeA,
				},
			},
		},
		{
			title:           "ingress rules with single tls having single hostname",
			targetNamespace: "",
			ingressItems: []legacyFakeIngress{
				{
					name:        "fake1",
					namespace:   namespace,
					tlsdnsnames: [][]string{{"example.org"}},
					ips:         []string{"1.2.3.4"},
				},
			},
			expected: []*endpoint.Endpoint{
				{
					DNSName:    "example.org",
					Targets:    endpoint.Targets{"1.2.3.4"},
					RecordType: endpoint.RecordTypeA,
				},
			},
		},
		{
			title:           "ingress rules with single tls having multiple hostnames",
			targetNamespace: "",
			ingressItems: []legacyFakeIngress{
				{
					name:        "fake1",
					namespace:   namespace,
					tlsdnsnames: [][]string{{"example.org", "example2.org"}},
					ips:         []string{"1.2.3.4"},
				},
			},
			expected: []*endpoint.Endpoint{
				{
					DNSName:    "example.org",
					Targets:    endpoint.Targets{"1.2.3.4"},
					RecordType: endpoint.RecordTypeA,
				},
				{
					DNSName:    "example2.org",
					Targets:    endpoint.Targets{"1.2.3.4"},
					RecordType: endpoint.RecordTypeA,
				},
			},
		},
		{
			title:           "ingress rules with multiple tls having multiple hostnames",
			targetNamespace: "",
			ingressItems: []legacyFakeIngress{
				{
					name:        "fake1",
					namespace:   namespace,
					tlsdnsnames: [][]string{{"example.org", "example2.org"}, {"example3.org", "example4.org"}},
					ips:         []string{"1.2.3.4"},
				},
			},
			expected: []*endpoint.Endpoint{
				{
					DNSName:    "example.org",
					Targets:    endpoint.Targets{"1.2.3.4"},
					RecordType: endpoint.RecordTypeA,
				},
				{
					DNSName:    "example2.org",
					Targets:    endpoint.Targets{"1.2.3.4"},
					RecordType: endpoint.RecordTypeA,
				},
				{
					DNSName:    "example3.org",
					Targets:    endpoint.Targets{"1.2.3.4"},
					RecordType: endpoint.RecordTypeA,
				},
				{
					DNSName:    "example4.org",
					Targets:    endpoint.Targets{"1.2.3.4"},
					RecordType: endpoint.RecordTypeA,
				},
			},
		},
		{
			title:           "ingress rules with hostname annotation",
			targetNamespace: "",
			ingressItems: []legacyFakeIngress{
				{
					name:      "fake1",
					namespace: namespace,
					annotations: map[string]string{
						hostnameAnnotationKey: "dns-through-hostname.com",
					},
					dnsnames: []string{"example.org"},
					ips:      []string{"1.2.3.4"},
				},
			},
			expected: []*endpoint.Endpoint{
				{
					DNSName:    "example.org",
					Targets:    endpoint.Targets{"1.2.3.4"},
					RecordType: endpoint.RecordTypeA,
				},
				{
					DNSName:    "dns-through-hostname.com",
					Targets:    endpoint.Targets{"1.2.3.4"},
					RecordType: endpoint.RecordTypeA,
				},
			},
		},
		{
			title:           "ingress rules with hostname annotation having multiple hostnames",
			targetNamespace: "",
			ingressItems: []legacyFakeIngress{
				{
					name:      "fake1",
					namespace: namespace,
					annotations: map[string]string{
						hostnameAnnotationKey: "dns-through-hostname.com, another-dns-through-hostname.com",
					},
					dnsnames: []string{"example.org"},
					ips:      []string{"1.2.3.4"},
				},
			},
			expected: []*endpoint.Endpoint{
				{
					DNSName:    "example.org",
					Targets:    endpoint.Targets{"1.2.3.4"},
					RecordType: endpoint.RecordTypeA,
				},
				{
					DNSName:    "dns-through-hostname.com",
					Targets:    endpoint.Targets{"1.2.3.4"},
					RecordType: endpoint.RecordTypeA,
				},
				{
					DNSName:    "another-dns-through-hostname.com",
					Targets:    endpoint.Targets{"1.2.3.4"},
					RecordType: endpoint.RecordTypeA,
				},
			},
		},
		{
			title:           "ingress rules with hostname and target annotation",
			targetNamespace: "",
			ingressItems: []legacyFakeIngress{
				{
					name:      "fake1",
					namespace: namespace,
					annotations: map[string]string{
						hostnameAnnotationKey: "dns-through-hostname.com",
						targetAnnotationKey:   "ingress-target.com",
					},
					dnsnames: []string{"example.org"},
					ips:      []string{},
				},
			},
			expected: []*endpoint.Endpoint{
				{
					DNSName:    "example.org",
					Targets:    endpoint.Targets{"ingress-target.com"},
					RecordType: endpoint.RecordTypeCNAME,
				},
				{
					DNSName:    "dns-through-hostname.com",
					Targets:    endpoint.Targets{"ingress-target.com"},
					RecordType: endpoint.RecordTypeCNAME,
				},
			},
		},
		{
			title:           "ingress rules with annotation and custom TTL",
			targetNamespace: "",
			ingressItems: []legacyFakeIngress{
				{
					name:      "fake1",
					namespace: namespace,
					annotations: map[string]string{
						targetAnnotationKey: "ingress-target.com",
						ttlAnnotationKey:    "6",
					},
					dnsnames: []string{"example.org"},
					ips:      []string{},
				},
				{
					name:      "fake2",
					namespace: namespace,
					annotations: map[string]string{
						targetAnnotationKey: "ingress-target.com",
						ttlAnnotationKey:    "1",
					},
					dnsnames: []string{"example2.org"},
					ips:      []string{"8.8.8.8"},
				},
				{
					name:      "fake3",
					namespace: namespace,
					annotations: map[string]string{
						targetAnnotationKey: "ingress-target.com",
						ttlAnnotationKey:    "10s",
					},
					dnsnames: []string{"example3.org"},
					ips:      []string{"8.8.4.4"},
				},
			},
			expected: []*endpoint.Endpoint{
				{
					DNSName:   "example.org",
					Targets:   endpoint.Targets{"ingress-target.com"},
					RecordTTL: endpoint.TTL(6),
				},
				{
					DNSName:   "example2.org",
					Targets:   endpoint.Targets{"ingress-target.com"},
					RecordTTL: endpoint.TTL(1),
				},
				{
					DNSName:   "example3.org",
					Targets:   endpoint.Targets{"ingress-target.com"},
					RecordTTL: endpoint.TTL(10),
				},
			},
		},
		{
			title:           "ingress rules with alias and target annotation",
			targetNamespace: "",
			ingressItems: []legacyFakeIngress{
				{
					name:      "fake1",
					namespace: namespace,
					annotations: map[string]string{
						targetAnnotationKey: "ingress-target.com",
						aliasAnnotationKey:  "true",
					},
					dnsnames: []string{"example.org"},
					ips:      []string{},
				},
			},
			expected: []*endpoint.Endpoint{
				{
					DNSName:    "example.org",
					Targets:    endpoint.Targets{"ingress-target.com"},
					RecordType: endpoint.RecordTypeCNAME,
					ProviderSpecific: endpoint.ProviderSpecific{{
						Name: "alias", Value: "true",
					}},
				},
			},
		},
		{
			title:           "ingress rules with alias set false and target annotation",
			targetNamespace: "",
			ingressItems: []legacyFakeIngress{
				{
					name:      "fake1",
					namespace: namespace,
					annotations: map[string]string{
						targetAnnotationKey: "ingress-target.com",
						aliasAnnotationKey:  "false",
					},
					dnsnames: []string{"example.org"},
					ips:      []string{},
				},
			},
			expected: []*endpoint.Endpoint{
				{
					DNSName:    "example.org",
					Targets:    endpoint.Targets{"ingress-target.com"},
					RecordType: endpoint.RecordTypeCNAME,
				},
			},
		},
		{
			title:           "template for ingress with annotation",
			targetNamespace: "",
			ingressItems: []legacyFakeIngress{
				{
					name:      "fake1",
					namespace: namespace,
					annotations: map[string]string{
						targetAnnotationKey: "ingress-target.com",
					},
					dnsnames:  []string{},
					ips:       []string{},
					hostnames: []string{},
				},
				{
					name:      "fake2",
					namespace: namespace,
					annotations: map[string]string{
						targetAnnotationKey: "ingress-target.com",
					},
					dnsnames: []string{},
					ips:      []string{"8.8.8.8"},
				},
				{
					name:      "fake3",
					namespace: namespace,
					annotations: map[string]string{
						targetAnnotationKey: "1.2.3.4",
					},
					dnsnames:  []string{},
					ips:       []string{},
					hostnames: []string{},
				},
			},
			expected: []*endpoint.Endpoint{
				{
					DNSName:    "fake1.ext-dns.test.com",
					Targets:    endpoint.Targets{"ingress-target.com"},
					RecordType: endpoint.RecordTypeCNAME,
				},
				{
					DNSName:    "fake2.ext-dns.test.com",
					Targets:    endpoint.Targets{"ingress-target.com"},
					RecordType: endpoint.RecordTypeCNAME,
				},
				{
					DNSName:    "fake3.ext-dns.test.com",
					Targets:    endpoint.Targets{"1.2.3.4"},
					RecordType: endpoint.RecordTypeA,
				},
			},
			fqdnTemplate: "{{.Name}}.ext-dns.test.com",
		},
		{
			title:           "Ingress with empty annotation",
			targetNamespace: "",
			ingressItems: []legacyFakeIngress{
				{
					name:      "fake1",
					namespace: namespace,
					annotations: map[string]string{
						targetAnnotationKey: "",
					},
					dnsnames:  []string{},
					ips:       []string{},
					hostnames: []string{},
				},
			},
			expected:     []*endpoint.Endpoint{},
			fqdnTemplate: "{{.Name}}.ext-dns.test.com",
		},
		{
			title:                    "ignore hostname annotation",
			targetNamespace:          "",
			ignoreHostnameAnnotation: true,
			ingressItems: []legacyFakeIngress{
				{
					name:      "fake1",
					namespace: namespace,
					dnsnames:  []string{"example.org"},
					ips:       []string{"8.8.8.8"},
				},
				{
					name:      "fake2",
					namespace: namespace,
					annotations: map[string]string{
						hostnameAnnotationKey: "dns-through-hostname.com",
					},
					dnsnames:  []string{"new.org"},
					hostnames: []string{"lb.com"},
				},
			},
			expected: []*endpoint.Endpoint{
				{
					DNSName: "example.org",
					Targets: endpoint.Targets{"8.8.8.8"},
				},
				{
					DNSName: "new.org",
					Targets: endpoint.Targets{"lb.com"},
				},
			},
		},
		{
			title:                "ignore tls section",
			targetNamespace:      "",
			ignoreIngressTLSSpec: true,
			ingressItems: []legacyFakeIngress{
				{
					name:        "fake1",
					namespace:   namespace,
					tlsdnsnames: [][]string{{"example.org"}},
					ips:         []string{"1.2.3.4"},
				},
			},
			expected: []*endpoint.Endpoint{},
		},
		{
			title:                "reading tls section",
			targetNamespace:      "",
			ignoreIngressTLSSpec: false,
			ingressItems: []legacyFakeIngress{
				{
					name:        "fake1",
					namespace:   namespace,
					tlsdnsnames: [][]string{{"example.org"}},
					ips:         []string{"1.2.3.4"},
				},
			},
			expected: []*endpoint.Endpoint{
				{
					DNSName: "example.org",
					Targets: endpoint.Targets{"1.2.3.4"},
				},
			},
		},
		{
			ingressLabelSelector: labels.SelectorFromSet(labels.Set{"app": "web-external"}),
			title:                "ingress with matching labels",
			targetNamespace:      "",
			ingressItems: []legacyFakeIngress{
				{
					name:      "fake1",
					namespace: namespace,
					dnsnames:  []string{"example.org"},
					ips:       []string{"8.8.8.8"},
					labels:    map[string]string{"app": "web-external", "name": "reverse-proxy"},
				},
			},
			expected: []*endpoint.Endpoint{
				{
					DNSName: "example.org",
					Targets: endpoint.Targets{"8.8.8.8"},
				},
			},
		},
		{
			ingressLabelSelector: labels.SelectorFromSet(labels.Set{"app": "web-external"}),
			title:                "ingress without matching labels",
			targetNamespace:      "",
			ingressItems: []legacyFakeIngress{
				{
					name:      "fake1",
					namespace: namespace,
					dnsnames:  []string{"example.org"},
					ips:       []string{"8.8.8.8"},
					labels:    map[string]string{"app": "web-internal", "name": "reverse-proxy"},
				},
			},
			expected: []*endpoint.Endpoint{},
		},
	} {
		ti := ti
		t.Run(ti.title, func(t *testing.T) {
			t.Parallel()

			fakeClient := fake.NewSimpleClientset()
			for _, item := range ti.ingressItems {
				ingress := item.Ingress()
				_, err := fakeClient.ExtensionsV1beta1().Ingresses(ingress.Namespace).Create(context.Background(), ingress, metav1.CreateOptions{})
				require.NoError(t, err)
			}

			if ti.ingressLabelSelector == nil {
				ti.ingressLabelSelector = labels.Everything()
			}

			source, _ := NewIngressSource(
				context.TODO(),
				fakeClient,
				ti.targetNamespace,
				ti.annotationFilter,
				ti.fqdnTemplate,
				ti.combineFQDNAndAnnotation,
				ti.ignoreHostnameAnnotation,
				ti.ignoreIngressTLSSpec,
				ti.ignoreIngressRulesSpec,
				ti.ingressLabelSelector,
			)
			// Informer cache has all of the ingresses. Retrieve and validate their endpoints.
			res, err := source.Endpoints(context.Background())
			if ti.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			validateEndpoints(t, res, ti.expected)
		})
	}
}

// ingress specific helper functions
type legacyFakeIngress struct {
	dnsnames    []string
	tlsdnsnames [][]string
	ips         []string
	hostnames   []string
	namespace   string
	name        string
	annotations map[string]string
	labels      map[string]string
}

func (ing legacyFakeIngress) Ingress() *extensionsv1beta1.Ingress {
	ingress := &extensionsv1beta1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:   ing.namespace,
			Name:        ing.name,
			Annotations: ing.annotations,
			Labels:      ing.labels,
		},
		Spec: extensionsv1beta1.IngressSpec{
			Rules: []extensionsv1beta1.IngressRule{},
		},
		Status: extensionsv1beta1.IngressStatus{
			LoadBalancer: v1.LoadBalancerStatus{
				Ingress: []v1.LoadBalancerIngress{},
			},
		},
	}
	for _, dnsname := range ing.dnsnames {
		ingress.Spec.Rules = append(ingress.Spec.Rules, extensionsv1beta1.IngressRule{
			Host: dnsname,
		})
	}
	for _, hosts := range ing.tlsdnsnames {
		ingress.Spec.TLS = append(ingress.Spec.TLS, extensionsv1beta1.IngressTLS{
			Hosts: hosts,
		})
	}
	for _, ip := range ing.ips {
		ingress.Status.LoadBalancer.Ingress = append(ingress.Status.LoadBalancer.Ingress, v1.LoadBalancerIngress{
			IP: ip,
		})
	}
	for _, hostname := range ing.hostnames {
		ingress.Status.LoadBalancer.Ingress = append(ingress.Status.LoadBalancer.Ingress, v1.LoadBalancerIngress{
			Hostname: hostname,
		})
	}
	return ingress
}