package normalization

import (
	"encoding/json"
	"fmt"
	"strings"

	log "github.com/ViaQ/logerr/v2/log/static"
	"github.com/google/go-cmp/cmp"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	logging "github.com/openshift/cluster-logging-operator/apis/logging/v1"
	"github.com/openshift/cluster-logging-operator/test/framework/functional"
	testfw "github.com/openshift/cluster-logging-operator/test/functional"
	"github.com/openshift/cluster-logging-operator/test/helpers/types"
	. "github.com/openshift/cluster-logging-operator/test/matchers"
)

const (
	Json = `
    {
      "a": "Alpha",
      "b": true,
      "c": 12345,
      "e": {
        "one": 1,
        "two": 2,
        "three": [
          3
        ],
        "four": [
          0,
          1,
          2,
          3,
          4
        ]
      },
      "h": {
        "a": {
          "b": {
            "c": {
              "d": {
                "e": {
                  "f": {
                    "g": 1
                  }
                }
              }
            }
          }
        }
      }
    }
    `
)

var _ = Describe("[Functional][Normalization] Json log parsing", func() {
	var (
		framework       *functional.CollectorFunctionalFramework
		clfb            *functional.ClusterLogForwarderBuilder
		expected        map[string]interface{}
		empty           map[string]interface{}
		expectedMessage string
		normalizeJson   = func(json string) string {
			json = strings.TrimSpace(strings.ReplaceAll(json, "\n", ""))
			json = strings.ReplaceAll(json, "\t", "")
			return strings.ReplaceAll(json, " ", "")
		}
	)

	const IndexName = "app-foo-write"

	BeforeEach(func() {
		empty = map[string]interface{}{}
		framework = functional.NewCollectorFunctionalFrameworkUsingCollector(testfw.LogCollectionType)
		clfb = functional.NewClusterLogForwarderBuilder(framework.Forwarder).
			FromInput(logging.InputNameApplication).
			ToOutputWithVisitor(func(output *logging.OutputSpec) {
				output.Elasticsearch.StructuredTypeName = "foo"
			}, logging.OutputTypeElasticsearch)

		expectedMessage = normalizeJson(Json)
	})
	AfterEach(func() {
		framework.Cleanup()
	})
	It("should parse json message into structured", func() {
		ExpectOK(json.Unmarshal([]byte(Json), &expected))

		readLogs := func() ([]types.ApplicationLog, error) {
			raw, err := framework.GetLogsFromElasticSearchIndex(logging.OutputTypeElasticsearch, IndexName)
			Expect(err).To(BeNil(), "Expected no errors reading the logs")

			// Parse log line
			var logs []types.ApplicationLog
			err = types.StrictlyParseLogsFromSlice(raw, &logs)
			Expect(err).To(BeNil(), "Expected no errors parsing the logs")
			return logs, err
		}

		if testfw.LogCollectionType == logging.LogCollectionTypeFluentd {
			framework = functional.NewCollectorFunctionalFrameworkUsingCollector(logging.LogCollectionTypeFluentd)
			clfb = functional.NewClusterLogForwarderBuilder(framework.Forwarder).
				FromInput(logging.InputNameApplication).
				ToFluentForwardOutput()
			readLogs = func() ([]types.ApplicationLog, error) {
				return framework.ReadApplicationLogsFrom(logging.OutputTypeFluentdForward)
			}
		}

		clfb.Forwarder.Spec.Pipelines[0].Parse = "json"
		ExpectOK(framework.Deploy())

		// Log message data
		applicationLogLine := functional.CreateAppLogFromJson(Json)
		Expect(framework.WriteMessagesToApplicationLog(applicationLogLine, 10)).To(BeNil())

		logs, err := readLogs()
		Expect(err).To(BeNil(), "Expected no errors reading the logs")

		log.V(2).Info("Received", "Message", logs[0].Message)
		Expect(logs[0].Message).To(BeEmpty())

		same := cmp.Equal(logs[0].Structured, expected)
		if !same {
			diff := cmp.Diff(logs[0].Structured, expected)
			fmt.Printf("diff %s\n", diff)
		}
		Expect(same).To(BeTrue(), "parsed json message not matching")
	})

	It("should not parse non json message into structured", func() {
		clfb.Forwarder.Spec.Pipelines[0].Parse = "json"
		Expect(framework.Deploy()).To(BeNil())

		// Log message data
		message := "Functional test message"
		timestamp := "2020-11-04T18:13:59.061892+00:00"

		applicationLogLine := fmt.Sprintf("%s stdout F %s", timestamp, message)
		Expect(framework.WriteMessagesToApplicationLog(applicationLogLine, 10)).To(BeNil())

		logs, err := framework.ReadApplicationLogsFrom(logging.OutputTypeElasticsearch)
		Expect(err).To(BeNil(), "Expected no errors reading the logs")

		same := cmp.Equal(logs[0].Structured, empty)
		if !same {
			diff := cmp.Diff(logs[0].Structured, empty)
			log.V(0).Info("Parsed json not as expected", "diff", diff)
		}
		Expect(same).To(BeFalse(), "parsed json message not matching")
		Expect(logs[0].Message).To(Equal(message), "received message not matching")
	})
	It("should not parse json if not configured", func() {
		// Pipeline.Parse is not set
		ExpectOK(framework.Deploy())

		applicationLogLine := functional.CreateAppLogFromJson(expectedMessage)
		Expect(framework.WriteMessagesToApplicationLog(applicationLogLine, 10)).To(BeNil())

		logs, err := framework.ReadApplicationLogsFrom(logging.OutputTypeElasticsearch)
		Expect(err).To(BeNil(), "Expected no errors reading the logs")

		Expect(logs[0].Structured).To(BeNil(), "expected nil structured field")
		Expect(logs[0].Message).To(Equal(expectedMessage), "received message not matching")
	})
	It("should not parse invalid json message into structured", func() {
		// This test case is disabled to fix the behavior of invalid json parsing
		clfb.Forwarder.Spec.Pipelines[0].Parse = "json"
		Expect(framework.Deploy()).To(BeNil())

		// Log message data
		invalidJson := `{"key":"v}`
		timestamp := "2020-11-04T18:13:59.061892+00:00"

		expectedMessage := invalidJson
		applicationLogLine := fmt.Sprintf("%s stdout F %s", timestamp, expectedMessage)
		Expect(framework.WriteMessagesToApplicationLog(applicationLogLine, 1)).To(BeNil())

		logs, err := framework.ReadApplicationLogsFrom(logging.OutputTypeElasticsearch)
		Expect(err).To(BeNil(), "Expected no errors reading the logs")

		same := cmp.Equal(logs[0].Structured, empty)
		if !same {
			diff := cmp.Diff(logs[0].Structured, empty)
			log.V(3).Info("Parsed json not as expected", "diff", diff)
		}
		Expect(logs[0].Message).To(Equal(expectedMessage), "received message not matching")
	})

	It("should verify LOG-2105 parses json message into structured field and writes to Elasticsearch", func() {
		framework = functional.NewCollectorFunctionalFrameworkUsingCollector(testfw.LogCollectionType)
		clfb = functional.NewClusterLogForwarderBuilder(framework.Forwarder).
			FromInput(logging.InputNameApplication).
			ToElasticSearchOutput()

		structuredTypeName := "kubernetes.namespace_name"
		clfb.Forwarder.Spec.Pipelines[0].Parse = "json"
		clfb.Forwarder.Spec.Outputs[0].Elasticsearch = &logging.Elasticsearch{
			ElasticsearchStructuredSpec: logging.ElasticsearchStructuredSpec{
				StructuredTypeName: structuredTypeName,
			},
		}

		ExpectOK(framework.Deploy())

		// Log message data
		sample := `{"timeUnixNano":1666278690764363000,"severityText":"error","severityNumber":17,"body":{"stringValue":"E1020 15:11:30.764269       1 timeout.go:137] post-timeout activity - time-elapsed: 1.321595349s, GET \"/readyz\" result: <nil>"},"resource":{"log":{"file":{"path":"/var/log/pods/openshift-oauth-apiserver_apiserver-b477bc494-hmj4r_998cef46-bccd-437e-9727-4d6389436885/oauth-apiserver/6.log"}},"host":{"name":"oscar7"},"container":{"name":"oauth-apiserver","id":"cri-o://ae8d9c8e46defc9ff1df9a35076e04e7bd6be2d9056500570bf9e3f85e9d6885","image":{"name":"quay.io/openshift-release-dev/ocp-v4.0-art-dev@sha256","tag":":ed90fd557cc619f98a99bc8c552ee7b8a8ee369a3a2cdf2f9a4727878d2d049e"}},"k8s":{"pod":{"name":"apiserver-b477bc494-hmj4r","uid":"998cef46-bccd-437e-9727-4d6389436885","ip":"10.128.0.33","owner":"ReplicaSet/apiserver-b477bc494","annotations":{"k8s.ovn.org/pod-networks":"{\"default\":{\"ip_addresses\":[\"10.128.0.33/23\"],\"mac_address\":\"0a:58:0a:80:00:21\",\"gateway_ips\":[\"10.128.0.1\"],\"ip_address\":\"10.128.0.33/23\",\"gateway_ip\":\"10.128.0.1\"}}","k8s.v1.cni.cncf.io/network-status":"[{\n    \"name\": \"ovn-kubernetes\",\n    \"interface\": \"eth0\",\n    \"ips\": [\n        \"10.128.0.33\"\n    ],\n    \"mac\": \"0a:58:0a:80:00:21\",\n    \"default\": true,\n    \"dns\": {}\n}]","k8s.v1.cni.cncf.io/networks-status":"[{\n    \"name\": \"ovn-kubernetes\",\n    \"interface\": \"eth0\",\n    \"ips\": [\n        \"10.128.0.33\"\n    ],\n    \"mac\": \"0a:58:0a:80:00:21\",\n    \"default\": true,\n    \"dns\": {}\n}]","openshift.io/scc":"privileged","operator.openshift.io/dep-openshift-oauth-apiserver.etcd-client.secret":"OFllOQ==","operator.openshift.io/dep-openshift-oauth-apiserver.etcd-serving-ca.configmap":"f1B6eQ=="},"labels":{"apiserver":"true","app":"openshift-oauth-apiserver","oauth-apiserver-anti-affinity":"true","pod-template-hash":"b477bc494","revision":"2"}},"namespace":{"name":"openshift-oauth-apiserver","labels":{"kubernetes.io/metadata.name":"openshift-oauth-apiserver","olm.operatorgroup.uid/d5ae8d2e-99f3-4020-998d-9fc74c33faeb":"","openshift.io/cluster-monitoring":"true","pod-security.kubernetes.io/audit":"privileged","pod-security.kubernetes.io/enforce":"privileged","pod-security.kubernetes.io/warn":"privileged"}}},"attributes":[{"key":"log_type","value":"infrastructure"}]}}`
		expectedMessage = normalizeJson(sample)
		expected = map[string]interface{}{}
		_ = json.Unmarshal([]byte(sample), &expected)

		applicationLogLine := functional.CreateAppLogFromJson(sample)
		Expect(framework.WriteMessagesToApplicationLog(applicationLogLine, 1)).To(BeNil())

		// Read line from Log Forward output
		raw, err := framework.GetLogsFromElasticSearchIndex(logging.OutputTypeElasticsearch, fmt.Sprintf("app-%s-write", structuredTypeName))
		Expect(err).To(BeNil(), "Expected no errors reading the logs")
		Expect(raw).To(Not(BeEmpty()))

		// Parse log line
		var logs []types.ApplicationLog
		err = types.StrictlyParseLogsFromSlice(raw, &logs)
		Expect(err).To(BeNil(), "Expected no errors parsing the logs")
		Expect(logs).To(HaveLen(1), "Expected to receive the log message")
		Expect(logs[0].Structured).To(Equal(expected), "structured field with parsed json message not matching")
		log.V(2).Info("Received", "Message", logs[0])
		Expect(logs[0].Message).To(BeEmpty())
	})

	It("should not parse raise warn message in collector container : LOG-1806", func() {
		// This test case is disabled to fix the behavior of invalid json parsing
		clfb.Forwarder.Spec.Pipelines[0].Parse = "json"
		Expect(framework.Deploy()).To(BeNil())

		// Log message data
		invalidJson := `{"key":"v}`
		timestamp := "2020-11-04T18:13:59.061892+00:00"

		// Write log line as input to fluentd
		expectedMessage := invalidJson
		applicationLogLine := fmt.Sprintf("%s stdout F %s", timestamp, expectedMessage)
		Expect(framework.WriteMessagesToApplicationLog(applicationLogLine, 1)).To(BeNil())

		// Read line from Log Forward output
		_, err := framework.ReadApplicationLogsFrom(logging.OutputTypeElasticsearch)
		Expect(err).To(BeNil(), "Expected no errors reading the logs")

		output, err := framework.GetLogsFromCollector()
		Expect(err).To(BeNil(), "Expected no errors reading the logs from collector")
		Expect(strings.Contains(output, "class=Fluent::Plugin::Parser::ParserError error=\"pattern not matched with data")).To(BeFalse())
	})
})
