package k8shandler

import (
	"reflect"
	"testing"

	logging "github.com/openshift/cluster-logging-operator/pkg/apis/logging/v1"
	"github.com/openshift/cluster-logging-operator/pkg/utils"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

func TestNewRsyslogPodSpecWhenSelectorIsDefined(t *testing.T) {
	expSelector := map[string]string{
		"foo": "bar",
	}
	cluster := &logging.ClusterLogging{
		Spec: logging.ClusterLoggingSpec{
			Collection: logging.CollectionSpec{
				logging.LogCollectionSpec{
					Type: "rsyslog",
					RsyslogSpec: logging.RsyslogSpec{
						NodeSelector: expSelector,
					},
				},
			},
		},
	}
	podSpec := newRsyslogPodSpec(cluster, "test-app-name", "test-infra-name")

	if !reflect.DeepEqual(podSpec.NodeSelector, expSelector) {
		t.Errorf("Exp. the nodeSelector to be %q but was %q", expSelector, podSpec.NodeSelector)
	}
}

func TestNewRsyslogPodSpecWhenFieldsAreUndefined(t *testing.T) {

	cluster := &logging.ClusterLogging{}
	podSpec := newRsyslogPodSpec(cluster, "test-app-name", "test-infra-name")

	if len(podSpec.Containers) != 2 {
		t.Error("Exp. there to be 2 rsyslog containers")
	}

	resources := podSpec.Containers[0].Resources
	if resources.Limits[v1.ResourceMemory] != defaultRsyslogMemory {
		t.Errorf("Exp. the default memory limit to be %v", defaultRsyslogMemory)
	}
	if resources.Requests[v1.ResourceMemory] != defaultRsyslogMemory {
		t.Errorf("Exp. the default memory request to be %v", defaultRsyslogMemory)
	}
	if resources.Requests[v1.ResourceCPU] != defaultFluentdCpuRequest {
		t.Errorf("Exp. the default CPU request to be %v", defaultRsyslogCpuRequest)
	}
	CheckIfThereIsOnlyTheLinuxSelector(podSpec, t)
}

func TestRsyslogPodSpecHasTaintTolerations(t *testing.T) {

	expectedTolerations := []v1.Toleration{
		v1.Toleration{
			Key:      "node-role.kubernetes.io/master",
			Operator: v1.TolerationOpExists,
			Effect:   v1.TaintEffectNoSchedule,
		},
		v1.Toleration{
			Key:      "node.kubernetes.io/disk-pressure",
			Operator: v1.TolerationOpExists,
			Effect:   v1.TaintEffectNoSchedule,
		},
	}

	cluster := &logging.ClusterLogging{
		Spec: logging.ClusterLoggingSpec{
			Collection: logging.CollectionSpec{
				logging.LogCollectionSpec{
					Type: "rsyslog",
				},
			},
		},
	}
	podSpec := newFluentdPodSpec(cluster, "test-app-name", "test-infra-name")

	if !reflect.DeepEqual(podSpec.Tolerations, expectedTolerations) {
		t.Errorf("Exp. the tolerations to be %v but was %v", expectedTolerations, podSpec.Tolerations)
	}
}

func TestNewRsyslogPodSpecWhenResourcesAreDefined(t *testing.T) {
	limitMemory := resource.MustParse("100Gi")
	requestMemory := resource.MustParse("120Gi")
	requestCPU := resource.MustParse("500m")
	cluster := &logging.ClusterLogging{
		Spec: logging.ClusterLoggingSpec{
			Collection: logging.CollectionSpec{
				logging.LogCollectionSpec{
					Type: "rsyslog",
					RsyslogSpec: logging.RsyslogSpec{
						Resources: newResourceRequirements("100Gi", "", "120Gi", "500m"),
					},
				},
			},
		},
	}
	podSpec := newRsyslogPodSpec(cluster, "test-app-name", "test-infra-name")

	if len(podSpec.Containers) != 2 {
		t.Error("Exp. there to be 2 rsyslog containers")
	}

	resources := podSpec.Containers[0].Resources
	if resources.Limits[v1.ResourceMemory] != limitMemory {
		t.Errorf("Exp. the spec memory limit to be %v", limitMemory)
	}
	if resources.Requests[v1.ResourceMemory] != requestMemory {
		t.Errorf("Exp. the spec memory request to be %v", requestMemory)
	}
	if resources.Requests[v1.ResourceCPU] != requestCPU {
		t.Errorf("Exp. the spec CPU request to be %v", requestCPU)
	}
}

func TestNewRsyslogPodNoTolerations(t *testing.T) {
	expTolerations := []v1.Toleration{
		v1.Toleration{
			Key:      "node-role.kubernetes.io/master",
			Operator: v1.TolerationOpExists,
			Effect:   v1.TaintEffectNoSchedule,
		},
		v1.Toleration{
			Key:      "node.kubernetes.io/disk-pressure",
			Operator: v1.TolerationOpExists,
			Effect:   v1.TaintEffectNoSchedule,
		},
	}

	cluster := &logging.ClusterLogging{
		Spec: logging.ClusterLoggingSpec{
			Collection: logging.CollectionSpec{
				logging.LogCollectionSpec{
					Type:        "rsyslog",
					RsyslogSpec: logging.RsyslogSpec{},
				},
			},
		},
	}

	podSpec := newRsyslogPodSpec(cluster, "test-app-name", "test-infra-name")
	tolerations := podSpec.Tolerations

	if !utils.AreTolerationsSame(tolerations, expTolerations) {
		t.Errorf("Exp. the tolerations to be %v but was %v", expTolerations, tolerations)
	}
}

func TestNewRsyslogPodWithTolerations(t *testing.T) {

	providedToleration := v1.Toleration{
		Key:      "test",
		Operator: v1.TolerationOpExists,
		Effect:   v1.TaintEffectNoSchedule,
	}

	expTolerations := []v1.Toleration{
		providedToleration,
		v1.Toleration{
			Key:      "node-role.kubernetes.io/master",
			Operator: v1.TolerationOpExists,
			Effect:   v1.TaintEffectNoSchedule,
		},
		v1.Toleration{
			Key:      "node.kubernetes.io/disk-pressure",
			Operator: v1.TolerationOpExists,
			Effect:   v1.TaintEffectNoSchedule,
		},
	}

	cluster := &logging.ClusterLogging{
		Spec: logging.ClusterLoggingSpec{
			Collection: logging.CollectionSpec{
				logging.LogCollectionSpec{
					Type: "rsyslog",
					RsyslogSpec: logging.RsyslogSpec{
						Tolerations: []v1.Toleration{
							providedToleration,
						},
					},
				},
			},
		},
	}

	podSpec := newRsyslogPodSpec(cluster, "test-app-name", "test-infra-name")
	tolerations := podSpec.Tolerations

	if !utils.AreTolerationsSame(tolerations, expTolerations) {
		t.Errorf("Exp. the tolerations to be %v but was %v", expTolerations, tolerations)
	}
}
