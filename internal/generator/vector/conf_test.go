package vector

import (
	_ "embed"
	"fmt"
	"strings"

	configv1 "github.com/openshift/api/config/v1"

	"github.com/openshift/cluster-logging-operator/internal/constants"
	"github.com/openshift/cluster-logging-operator/internal/tls"
	"github.com/openshift/cluster-logging-operator/test/matchers"

	testhelpers "github.com/openshift/cluster-logging-operator/test/helpers"

	"github.com/openshift/cluster-logging-operator/internal/generator"
	"github.com/openshift/cluster-logging-operator/internal/generator/vector/helpers"

	"github.com/google/go-cmp/cmp"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/gomega"
	logging "github.com/openshift/cluster-logging-operator/apis/logging/v1"
	corev1 "k8s.io/api/core/v1"
)

//go:embed conf_test/complex.toml
var ExpectedComplexToml string

//go:embed conf_test/complex_es_no_ver.toml
var ExpectedComplexEsNoVerToml string

//go:embed conf_test/complex_es_v6.toml
var ExpectedComplexEsV6Toml string

//go:embed conf_test/es_pipeline_w_spaces.toml
var ExpectedEsPipelineWSpacesToml string

// TODO: Use a detailed CLF spec
var _ = Describe("Testing Complete Config Generation", func() {
	var (
		f = func(testcase testhelpers.ConfGenerateTest) {
			g := generator.MakeGenerator()
			if testcase.Options == nil {
				testcase.Options = generator.Options{generator.ClusterTLSProfileSpec: tls.GetClusterTLSProfileSpec(nil)}
			}
			e := generator.MergeSections(Conf(&testcase.CLSpec, testcase.Secrets, &testcase.CLFSpec, constants.OpenshiftNS, testcase.Options))
			conf, err := g.GenerateConf(e...)
			Expect(err).To(BeNil())
			Expect(strings.TrimSpace(testcase.ExpectedConf)).To(matchers.EqualTrimLines(conf))
		}
	)

	DescribeTable("Generate full vector.toml", f,
		Entry("with complex spec", testhelpers.ConfGenerateTest{
			Options: generator.Options{
				generator.ClusterTLSProfileSpec: tls.GetClusterTLSProfileSpec(nil),
			},
			CLSpec: logging.CollectionSpec{
				Fluentd: &logging.FluentdForwarderSpec{
					Buffer: &logging.FluentdBufferSpec{
						ChunkLimitSize: "8m",
						TotalLimitSize: "800000000",
						OverflowAction: "throw_exception",
					},
				},
			},
			CLFSpec: logging.ClusterLogForwarderSpec{
				Inputs: []logging.InputSpec{
					{
						Name: "mytestapp",
						Application: &logging.Application{
							Namespaces: []string{"test-ns"},
						},
					},
				},
				Pipelines: []logging.PipelineSpec{
					{
						InputRefs: []string{
							"mytestapp",
							logging.InputNameInfrastructure,
							logging.InputNameAudit},
						OutputRefs: []string{"kafka-receiver"},
						Name:       "pipeline",
						Labels:     map[string]string{"key1": "value1", "key2": "value2"},
					},
				},
				Outputs: []logging.OutputSpec{
					{
						Type: logging.OutputTypeKafka,
						Name: "kafka-receiver",
						URL:  "tls://broker1-kafka.svc.messaging.cluster.local:9092/topic",
						Secret: &logging.OutputSecretSpec{
							Name: "kafka-receiver-1",
						},
					},
				},
			},
			Secrets: map[string]*corev1.Secret{
				"kafka-receiver": {
					Data: map[string][]byte{
						"tls.key":       []byte("junk"),
						"tls.crt":       []byte("junk"),
						"ca-bundle.crt": []byte("junk"),
					},
				},
			},
			ExpectedConf: ExpectedComplexToml,
		}),

		Entry("with complex spec for elasticsearch, without version specified", testhelpers.ConfGenerateTest{
			CLSpec: logging.CollectionSpec{},
			CLFSpec: logging.ClusterLogForwarderSpec{
				Pipelines: []logging.PipelineSpec{
					{
						InputRefs: []string{
							logging.InputNameApplication,
							logging.InputNameInfrastructure,
							logging.InputNameAudit},
						OutputRefs: []string{"es-1", "es-2"},
						Name:       "pipeline",
					},
				},
				Outputs: []logging.OutputSpec{
					{
						Type: logging.OutputTypeElasticsearch,
						Name: "es-1",
						URL:  "https://es-1.svc.messaging.cluster.local:9200",
						Secret: &logging.OutputSecretSpec{
							Name: "es-1",
						},
					},
					{
						Type: logging.OutputTypeElasticsearch,
						Name: "es-2",
						URL:  "https://es-2.svc.messaging.cluster.local:9200",
						Secret: &logging.OutputSecretSpec{
							Name: "es-2",
						},
					},
				},
			},
			Secrets: map[string]*corev1.Secret{
				"es-1": {
					Data: map[string][]byte{
						"tls.key":       []byte("junk"),
						"tls.crt":       []byte("junk"),
						"ca-bundle.crt": []byte("junk"),
					},
				},
				"es-2": {
					Data: map[string][]byte{
						"tls.key":       []byte("junk"),
						"tls.crt":       []byte("junk"),
						"ca-bundle.crt": []byte("junk"),
					},
				},
			},
			ExpectedConf: ExpectedComplexEsNoVerToml,
		}),

		Entry("with complex spec for elasticsearch default v6 & latest version", testhelpers.ConfGenerateTest{
			Options: generator.Options{
				generator.ClusterTLSProfileSpec: tls.GetClusterTLSProfileSpec(&configv1.TLSSecurityProfile{Type: configv1.TLSProfileIntermediateType}),
			},
			CLSpec: logging.CollectionSpec{},
			CLFSpec: logging.ClusterLogForwarderSpec{
				Pipelines: []logging.PipelineSpec{
					{
						InputRefs: []string{
							logging.InputNameApplication,
							logging.InputNameInfrastructure,
							logging.InputNameAudit},
						OutputRefs: []string{"default", "es-1", "es-2"},
						Name:       "pipeline",
					},
				},
				Outputs: []logging.OutputSpec{
					{
						Name:   logging.OutputNameDefault,
						Type:   logging.OutputTypeElasticsearch,
						URL:    constants.LogStoreURL,
						Secret: &logging.OutputSecretSpec{Name: constants.CollectorSecretName},
					},
					{
						Type: logging.OutputTypeElasticsearch,
						Name: "es-1",
						URL:  "https://es-1.svc.messaging.cluster.local:9200",
						OutputTypeSpec: logging.OutputTypeSpec{
							Elasticsearch: &logging.Elasticsearch{
								Version: logging.DefaultESVersion,
							},
						},
						Secret: &logging.OutputSecretSpec{
							Name: "es-1",
						},
						TLS: &logging.OutputTLSSpec{
							TLSSecurityProfile: &configv1.TLSSecurityProfile{Type: configv1.TLSProfileIntermediateType},
						},
					},
					{
						Type: logging.OutputTypeElasticsearch,
						Name: "es-2",
						URL:  "https://es-2.svc.messaging.cluster.local:9200",
						OutputTypeSpec: logging.OutputTypeSpec{
							Elasticsearch: &logging.Elasticsearch{
								Version: logging.FirstESVersionWithoutType,
							},
						},
						Secret: &logging.OutputSecretSpec{
							Name: "es-2",
						},
						TLS: &logging.OutputTLSSpec{
							TLSSecurityProfile: &configv1.TLSSecurityProfile{Type: configv1.TLSProfileIntermediateType},
						},
					},
				},
			},
			Secrets: map[string]*corev1.Secret{
				"es-1": {
					Data: map[string][]byte{
						"tls.key":       []byte("junk"),
						"tls.crt":       []byte("junk"),
						"ca-bundle.crt": []byte("junk"),
					},
				},
				"es-2": {
					Data: map[string][]byte{
						"tls.key":       []byte("junk"),
						"tls.crt":       []byte("junk"),
						"ca-bundle.crt": []byte("junk"),
					},
				},
			},
			ExpectedConf: ExpectedComplexEsV6Toml,
		}),

		Entry("with complex spec for elasticsearch, pipeline name with spaces", testhelpers.ConfGenerateTest{
			CLSpec: logging.CollectionSpec{},
			CLFSpec: logging.ClusterLogForwarderSpec{
				Pipelines: []logging.PipelineSpec{
					{
						InputRefs: []string{
							logging.InputNameApplication,
							logging.InputNameInfrastructure,
							logging.InputNameAudit},
						OutputRefs: []string{"es-1"},
						Name:       "pipeline with space",
					},
				},
				Outputs: []logging.OutputSpec{
					{
						Type: logging.OutputTypeElasticsearch,
						Name: "es-1",
						URL:  "https://es-1.svc.messaging.cluster.local:9200",
						Secret: &logging.OutputSecretSpec{
							Name: "es-1",
						},
					},
				},
			},
			Secrets: map[string]*corev1.Secret{
				"es-1": {
					Data: map[string][]byte{
						"tls.key":       []byte("junk"),
						"tls.crt":       []byte("junk"),
						"ca-bundle.crt": []byte("junk"),
					},
				},
			},
			ExpectedConf: ExpectedEsPipelineWSpacesToml,
		}),
	)

	Describe("test helper functions", func() {
		It("test MakeInputs", func() {
			diff := cmp.Diff(helpers.MakeInputs("a", "b"), "[\"a\",\"b\"]")
			fmt.Println(diff)
			Expect(diff).To(Equal(""))
		})
	})
})
