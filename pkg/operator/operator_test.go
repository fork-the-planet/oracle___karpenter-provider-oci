/*
** Karpenter Provider OCI
**
** Copyright (c) 2026 Oracle and/or its affiliates.
** Licensed under the Universal Permissive License v 1.0 as shown at https://oss.oracle.com/licenses/upl/
 */

package operator

import (
	"context"
	"crypto/rsa"
	"net/http"
	"testing"

	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/oracle/karpenter-provider-oci/pkg/fakes"
	"github.com/oracle/karpenter-provider-oci/pkg/oci"
	"github.com/oracle/karpenter-provider-oci/pkg/operator/options"
	"github.com/oracle/karpenter-provider-oci/pkg/providers/kms"
	"github.com/oracle/karpenter-provider-oci/pkg/providers/network"
	"github.com/oracle/oci-go-sdk/v65/common"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/version"
	fakediscovery "k8s.io/client-go/discovery/fake"
	k8sclientfake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/events"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/flowcontrol"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/config"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/conversion"
	"sigs.k8s.io/karpenter/pkg/operator"
)

func TestOperator(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "Operator Suite")
}

var _ = Describe("Test Operator", func() {
	It("test valueOrDefault method", func() {
		tests := []struct {
			name string
			val  string
			def  string
			want string
		}{
			{"non-empty val", "present", "default", "present"},
			{"empty val", "", "default", "default"},
			{"empty val, empty default", "", "", ""},
			{"non-empty val, empty default", "nonempty", "", "nonempty"},
		}
		for _, tt := range tests {
			got := valueOrDefault(tt.val, tt.def)
			GinkgoWriter.Print("Testing", tt.name)
			Expect(got).To(Equal(tt.want))
		}
	})

	It("should create operator properly", func() {
		testCases := []struct {
			authMethod options.AuthMethod
			want       common.AuthenticationType
		}{
			{options.AuthBySession, common.UnknownAuthenticationType},
			{options.AuthByInstancePrincipal, common.InstancePrincipal},
			{options.AuthByWORKLOAD, common.UnknownAuthenticationType},
		}

		nodeClassClusterCompartmentId := "ocid1.compartment.oc1..cluster123"

		mockOciClient := &oci.Client{
			Compute:  fakes.NewFakeComputeClient(nodeClassClusterCompartmentId),
			Identity: fakes.NewFakeIdentityClient(),
		}
		mockCoreOp := &operator.Operator{
			Manager: &MockManager{},
		}
		mockClientset := k8sclientfake.NewClientset()
		fakeVersion := &version.Info{
			Major:      "1",
			Minor:      "30",
			GitVersion: "v1.30.0",
		}

		mockClientset.Discovery().(*fakediscovery.FakeDiscovery).FakedServerVersion = fakeVersion
		restConfig := &rest.Config{
			TLSClientConfig: rest.TLSClientConfig{
				CAData: []byte("DUMMY_CA"),
			},
		}

		for _, tc := range testCases {
			ociOptions := &options.Options{
				OciVcnIpNative: true,
				IpFamiliesFlag: &network.IpFamilyValue{
					IpFamilies: []network.IpFamily{network.IPv4},
				},
				OciAuthMethods: tc.authMethod,
			}

			_, operator := createOperator(context.TODO(), mockCoreOp,
				ociOptions, "us-phoenix-1",
				mockOciClient, mockClientset, restConfig, &MockAuthConfigProvider{})

			Expect(operator).NotTo(BeNil())
			authType, err := operator.KmsKeyProvider.(*kms.DefaultProvider).GetConfigProvider().AuthType()
			Expect(err).ToNot(HaveOccurred())
			Expect(authType.AuthType).To(Equal(tc.want))
		}
	})
})

// Mock AuthConfigProvider
type MockAuthConfigProvider struct{}

func (p *MockAuthConfigProvider) GetCustomProfileSessionTokenConfigProvider(
	profileName string) common.ConfigurationProvider {
	return &MockConfigurationProvider{
		AuthenticationType: common.UnknownAuthenticationType,
	}
}

func (p *MockAuthConfigProvider) GetInstancePrincipalConfigurationProvider() (common.ConfigurationProvider, error) {
	return &MockConfigurationProvider{
		AuthenticationType: common.InstancePrincipal,
	}, nil
}

func (p *MockAuthConfigProvider) GetWorkloadIdentityConfigurationProvider(
	region string) (common.ConfigurationProvider, error) {
	return &MockConfigurationProvider{
		AuthenticationType: common.UnknownAuthenticationType,
	}, nil
}

// Mock ConfigurationProvider
type MockConfigurationProvider struct {
	AuthenticationType common.AuthenticationType
}

func (cp *MockConfigurationProvider) PrivateRSAKey() (*rsa.PrivateKey, error) {
	return &rsa.PrivateKey{}, nil
}
func (cp *MockConfigurationProvider) KeyID() (string, error) {
	return "testKeyID", nil
}

func (cp *MockConfigurationProvider) TenancyOCID() (string, error) {
	return "testTenancyOCID", nil
}

func (cp *MockConfigurationProvider) UserOCID() (string, error) {
	return "testUserOCID", nil
}

func (cp *MockConfigurationProvider) KeyFingerprint() (string, error) {
	return "testKeyFingerprint", nil
}

func (cp *MockConfigurationProvider) Region() (string, error) {
	return "us-phoenix-1", nil
}

func (cp *MockConfigurationProvider) AuthType() (common.AuthConfig, error) {
	return common.AuthConfig{
		AuthType: cp.AuthenticationType,
	}, nil
}

// Mock
type MockManager struct{}

func (m *MockManager) GetHTTPClient() *http.Client {
	return &http.Client{}
}

func (m *MockManager) GetConfig() *rest.Config {
	return &rest.Config{}
}

func (m *MockManager) GetCache() cache.Cache {
	return nil
}

func (m *MockManager) GetScheme() *runtime.Scheme {
	return &runtime.Scheme{}
}

func (m *MockManager) GetClient() client.Client {
	return nil
}

func (m *MockManager) GetFieldIndexer() client.FieldIndexer {
	return nil
}

func (m *MockManager) GetEventRecorderFor(name string) record.EventRecorder {
	return nil
}

func (m *MockManager) GetEventRecorder(name string) events.EventRecorder {
	return nil
}

func (m *MockManager) GetRESTMapper() meta.RESTMapper {
	return nil
}

func (m *MockManager) GetAPIReader() client.Reader {
	return nil
}

func (m *MockManager) Start(ctx context.Context) error {
	return nil
}

func (m *MockManager) Add(manager.Runnable) error {
	return nil
}

func (m *MockManager) Elected() <-chan struct{} {
	return fakes.NewDummyChannel()
}
func (m *MockManager) AddMetricsServerExtraHandler(path string, handler http.Handler) error {
	return nil
}

func (m *MockManager) AddHealthzCheck(name string, check healthz.Checker) error {
	return nil
}

func (m *MockManager) AddReadyzCheck(name string, check healthz.Checker) error {
	return nil
}

func (m *MockManager) GetWebhookServer() webhook.Server {
	return nil
}

func (m *MockManager) GetLogger() logr.Logger {
	return logr.Logger{}
}

func (m *MockManager) GetControllerOptions() config.Controller {
	return config.Controller{}
}

func (m *MockManager) GetConverterRegistry() conversion.Registry {
	return conversion.NewRegistry()
}

type MockRestInterface struct{}

func (ki *MockRestInterface) GetRateLimiter() flowcontrol.RateLimiter {
	return nil
}

func (ki *MockRestInterface) Verb(verb string) *rest.Request {
	return &rest.Request{}
}

func (ki *MockRestInterface) Post() *rest.Request {
	return &rest.Request{}
}

func (ki *MockRestInterface) Put() *rest.Request {
	return &rest.Request{}
}

func (ki *MockRestInterface) Patch(pt types.PatchType) *rest.Request {
	return &rest.Request{}
}

func (ki *MockRestInterface) Get() *rest.Request {
	return &rest.Request{}
}

func (ki *MockRestInterface) Delete() *rest.Request {
	return &rest.Request{}
}

func (ki *MockRestInterface) APIVersion() schema.GroupVersion {
	return schema.GroupVersion{}
}
