package functional

import (
	"context"
	"fmt"
	"github.com/openshift/cluster-logging-operator/internal/collector"
	"github.com/openshift/cluster-logging-operator/test"
	"github.com/openshift/cluster-logging-operator/test/helpers/certificate"
	"k8s.io/apimachinery/pkg/util/sets"
	"net"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/openshift/cluster-logging-operator/internal/pkg/generator/forwarder"
	"github.com/openshift/cluster-logging-operator/internal/runtime"
	testruntime "github.com/openshift/cluster-logging-operator/test/runtime"

	yaml "sigs.k8s.io/yaml"

	logger "github.com/ViaQ/logerr/v2/log"
	log "github.com/ViaQ/logerr/v2/log/static"
	logging "github.com/openshift/cluster-logging-operator/apis/logging/v1"
	"github.com/openshift/cluster-logging-operator/internal/constants"
	"github.com/openshift/cluster-logging-operator/internal/utils"
	"github.com/openshift/cluster-logging-operator/test/client"
	frameworkfluent "github.com/openshift/cluster-logging-operator/test/framework/functional/fluentd"
	frameworkvector "github.com/openshift/cluster-logging-operator/test/framework/functional/vector"
	"github.com/openshift/cluster-logging-operator/test/helpers/oc"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/util/wait"
)

var TestAPIAdapterConfigVisitor = func(conf string) string {
	conf = strings.Replace(conf, "@type kubernetes_metadata", "@type kubernetes_metadata\ntest_api_adapter  KubernetesMetadata::TestApiAdapter\n", 1)
	return conf
}

type CollectorFramework interface {
	DeployConfigMapForConfig(name, config, clfYaml string) error
	BuildCollectorContainer(*runtime.ContainerBuilder, string) *runtime.ContainerBuilder
	IsStarted(string) bool
	Image() string
	String() string
	ModifyConfig(string) string
}

// CollectorFunctionalFramework deploys stand alone fluentd with the fluent.conf as generated by input ClusterLogForwarder CR
type CollectorFunctionalFramework struct {
	Name              string
	Namespace         string
	Conf              string
	image             string
	Labels            map[string]string
	Forwarder         *logging.ClusterLogForwarder
	Test              *client.Test
	Pod               *corev1.Pod
	fluentContainerId string
	closeClient       func()

	//Secrets associated with outputs to mount into the collector podspec
	Secrets []*corev1.Secret

	collector CollectorFramework
	//VisitConfig allows the Framework to modify the config after generating from logforwardering
	VisitConfig func(string) string

	//MaxReadDuration is the max duration to wait to read logs from the receiver
	MaxReadDuration *time.Duration
}

func NewCollectorFunctionalFramework() *CollectorFunctionalFramework {
	test := client.NewTest()
	return NewCollectorFunctionalFrameworkUsing(test, test.Close, 0, logging.LogCollectionTypeFluentd)
}

func NewCollectorFunctionalFrameworkUsingCollector(logCollectorType logging.LogCollectionType, testOptions ...client.TestOption) *CollectorFunctionalFramework {
	test := client.NewTest(testOptions...)
	return NewCollectorFunctionalFrameworkUsing(test, test.Close, 0, logCollectorType)
}

func NewFluentdFunctionalFrameworkForTest(t *testing.T) *CollectorFunctionalFramework {
	return NewCollectorFunctionalFrameworkUsing(client.ForTest(t), func() {}, 0, logging.LogCollectionTypeFluentd)
}

func NewCollectorFunctionalFrameworkUsing(t *client.Test, fnClose func(), verbosity int, collectorType logging.LogCollectionType) *CollectorFunctionalFramework {
	if level, found := os.LookupEnv("LOG_LEVEL"); found {
		if i, err := strconv.Atoi(level); err == nil {
			verbosity = i
		}
	}
	var collectorImpl CollectorFramework = &frameworkfluent.FluentdCollector{
		Test: t,
	}
	if collectorType == logging.LogCollectionTypeVector {
		collectorImpl = &frameworkvector.VectorCollector{
			Test: t,
		}
	}

	log.SetLogger(logger.NewLogger("functional-Framework", logger.WithVerbosity(verbosity)))

	log.Info("Using collector", "impl", collectorImpl.String())

	testName := "functional"
	framework := &CollectorFunctionalFramework{
		Name:      testName,
		Namespace: t.NS.Name,
		image:     collectorImpl.Image(),
		Labels: map[string]string{
			"testtype": "functional",
			"testname": testName,
		},
		Test:        t,
		Forwarder:   testruntime.NewClusterLogForwarder(),
		closeClient: fnClose,
		collector:   collectorImpl,
	}
	framework.Forwarder.SetNamespace(t.NS.Name)
	return framework
}

// AddSecret to the framework to be created when Deploy is called
func (f *CollectorFunctionalFramework) AddSecret(secret *corev1.Secret) *CollectorFunctionalFramework {
	f.Secrets = append(f.Secrets, secret)
	return f
}

func (f *CollectorFunctionalFramework) Cleanup() {
	if g, ok := test.GinkgoCurrentTest(); ok && g.Failed {
		for _, container := range f.Pod.Spec.Containers {
			log.Info("Dumping logs for container", "container", container.Name)
			logs, err := oc.Logs().WithNamespace(f.Namespace).WithPod(f.Pod.Name).WithContainer(container.Name).Run()
			if err != nil {
				log.Error(err, "Unable to retrieve logs", "container", container.Name)
			}
			fmt.Println(logs)
		}
	}
	f.closeClient()
}

func (f *CollectorFunctionalFramework) GetMaxReadDuration() time.Duration {
	if f.MaxReadDuration != nil {
		return *f.MaxReadDuration
	}
	return maxDuration
}

func (f *CollectorFunctionalFramework) RunCommand(container string, cmd ...string) (string, error) {
	log.V(2).Info("Running", "container", container, "cmd", cmd)
	out, err := testruntime.ExecOc(f.Pod, strings.ToLower(container), cmd[0], cmd[1:]...)
	log.V(2).Info("Exec'd", "out", out, "err", err)
	return out, err
}

func (f *CollectorFunctionalFramework) AddOutputContainersVisitors() []runtime.PodBuilderVisitor {
	visitors := []runtime.PodBuilderVisitor{
		func(b *runtime.PodBuilder) error {
			return f.addOutputContainers(b, f.Forwarder.Spec.Outputs)
		},
	}
	return visitors
}

// Deploy the objects needed to functional Test
func (f *CollectorFunctionalFramework) Deploy() (err error) {
	return f.DeployWithVisitors(f.AddOutputContainersVisitors())
}

func (f *CollectorFunctionalFramework) DeployWithVisitor(visitor runtime.PodBuilderVisitor) (err error) {
	visitors := []runtime.PodBuilderVisitor{
		visitor,
	}
	return f.DeployWithVisitors(visitors)
}

// Deploy the objects needed to functional Test
func (f *CollectorFunctionalFramework) DeployWithVisitors(visitors []runtime.PodBuilderVisitor) (err error) {
	if err := f.deploySecrets(); err != nil {
		return err
	}
	log.V(2).Info("Generating config", "forwarder", f.Forwarder)
	clfYaml, _ := yaml.Marshal(f.Forwarder)
	debugOutput := false
	testClient := client.Get().ControllerRuntimeClient()
	if strings.TrimSpace(f.Conf) == "" {
		if f.Conf, err = forwarder.Generate(logging.LogCollectionType(f.collector.String()), string(clfYaml), false, debugOutput, testClient); err != nil {
			return err
		}
		//mock sources to facilitate functional testing
		f.Conf = f.collector.ModifyConfig(f.Conf)
	} else {
		log.V(2).Info("Using provided collector conf instead of generating one")
	}
	if f.VisitConfig != nil {
		log.V(2).Info("Modifying config using provided config visitor")
		f.Conf = f.VisitConfig(f.Conf)
	}

	if err = f.collector.DeployConfigMapForConfig(f.Name, f.Conf, string(clfYaml)); err != nil {
		return err
	}

	// Receiver acts as TLS server.
	privateCA := certificate.NewCA(nil, "Root CA")
	serverCert := certificate.NewCert(privateCA, "Server", fmt.Sprintf("%s.%s", f.Name, f.Namespace), "localhost", net.IPv4(127, 0, 0, 1), net.IPv6loopback)
	certsName := "certs-" + f.Name
	certs := runtime.NewConfigMap(f.Test.NS.Name, certsName, map[string]string{})
	runtime.NewConfigMapBuilder(certs).
		Add("tls.key", string(serverCert.PrivateKeyPEM())).
		Add("tls.crt", string(serverCert.CertificatePEM()))
	if err = f.Test.Client.Create(certs); err != nil {
		return err
	}

	log.V(2).Info("Creating service")
	service := runtime.NewService(f.Test.NS.Name, f.Name)
	runtime.NewServiceBuilder(service).
		AddServicePort(24231, 24231).
		WithSelector(f.Labels)
	if err = f.Test.Client.Create(service); err != nil {
		return err
	}

	role := runtime.NewClusterRole(fmt.Sprintf("%s-%s", f.Test.NS.Name, f.Name),
		v1.PolicyRule{
			Verbs:     []string{"list", "get", "watch"},
			Resources: []string{"pods", "namespaces", "nodes"},
			APIGroups: []string{""},
		},
	)
	if err = f.Test.Client.Create(role); err != nil {
		return err
	}
	rolebinding := runtime.NewClusterRoleBinding(fmt.Sprintf("%s-%s", f.Test.NS.Name, f.Name),
		v1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "ClusterRole",
			Name:     role.Name,
		},
		v1.Subject{
			Kind:      "ServiceAccount",
			Name:      "default",
			Namespace: f.Test.NS.Name,
		},
	)
	if err = f.Test.Client.Create(rolebinding); err != nil {
		return err
	}

	log.V(2).Info("Defining pod...")
	resources := corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU: resource.MustParse("500m"),
		},
	}
	f.Pod = runtime.NewPod(f.Test.NS.Name, f.Name)
	b := runtime.NewPodBuilder(f.Pod).
		WithLabels(f.Labels).
		AddConfigMapVolume("config", f.Name).
		AddConfigMapVolumeWithPermissions("entrypoint", f.Name, utils.GetInt32(0755)).
		AddConfigMapVolume("certs", certsName)
	b = f.collector.BuildCollectorContainer(
		b.AddContainer(constants.CollectorName, f.image).
			AddEnvVar("OPENSHIFT_CLUSTER_ID", f.Name).
			AddEnvVarFromFieldRef("POD_IPS", "status.podIPs").
			WithImagePullPolicy(corev1.PullAlways).ResourceRequirements(resources), FunctionalNodeName).
		End()
	for _, visit := range visitors {
		if err = visit(b); err != nil {
			return err
		}
	}

	addSecretVolumeMountsToCollector(&f.Pod.Spec, f.Secrets)
	collector.AddSecretVolumes(&f.Pod.Spec, f.Forwarder.Spec)

	log.V(2).Info("Creating pod", "pod", f.Pod)
	if err = f.Test.Client.Create(f.Pod); err != nil {
		return err
	}
	if err = f.Test.Client.Get(f.Pod); err != nil {
		return err
	}

	log.V(2).Info("waiting for pod to be ready")
	if err = oc.Literal().From("oc wait -n %s pod/%s --timeout=120s --for=condition=Ready", f.Test.NS.Name, f.Name).Output(); err != nil {
		if out, describeErr := oc.Literal().From("oc describe -n %s pod/%s ", f.Test.NS.Name, f.Name).Run(); describeErr == nil {
			log.Info("Describe of the test pod", "describe", out)
		} else {
			log.V(2).Error(describeErr, "Error trying to describe the functional pod")
		}

		return err
	}
	if err = f.Test.Client.Get(f.Pod); err != nil {
		return err
	}
	log.V(2).Info("waiting for service endpoints to be ready")
	err = wait.PollUntilContextTimeout(context.TODO(), time.Second*2, time.Second*10, true, func(cxt context.Context) (done bool, err error) {
		ips, err := oc.Get().WithNamespace(f.Test.NS.Name).Resource("endpoints", f.Name).OutputJsonpath("{.subsets[*].addresses[*].ip}").Run()
		if err != nil {
			return false, nil
		}
		// if there are IPs in the service endpoint, the service is available
		if strings.TrimSpace(ips) != "" {
			return true, nil
		}
		return false, nil
	})
	if err != nil {
		return fmt.Errorf("service could not be started")
	}
	log.V(2).Info("waiting for the collector to be ready")
	err = wait.PollUntilContextTimeout(context.TODO(), time.Second*2, time.Second*90, true, func(cxt context.Context) (done bool, err error) {
		output, err := oc.Literal().From("oc logs -n %s pod/%s -c %s", f.Test.NS.Name, f.Name, constants.CollectorName).Run()
		if err != nil {
			return false, nil
		}

		// if collector started successfully return success
		if f.collector.IsStarted(output) || debugOutput {
			return true, nil
		}
		return false, nil
	})
	if err != nil {
		return fmt.Errorf("collector did not start in the container")
	}
	for _, cs := range f.Pod.Status.ContainerStatuses {
		if cs.Name == constants.CollectorName {
			f.fluentContainerId = strings.TrimPrefix(cs.ContainerID, "cri-o://")
			break
		}
	}
	return nil
}

func addSecretVolumeMountsToCollector(podSpec *corev1.PodSpec, secrets []*corev1.Secret) {
	log.V(3).Info("#addSecretVolumeMountsToCollector", "containers", podSpec.Containers)
	names := sets.NewString()
	for _, s := range secrets {
		names.Insert(s.Name)
	}
	containers := []corev1.Container{}
	for i := range podSpec.Containers {
		if podSpec.Containers[i].Name == constants.CollectorName {
			log.V(3).Info("Adding secret volume mounts to collector container")
			collector.AddSecretVolumeMounts(&podSpec.Containers[i], names.List())
		}
		containers = append(containers, podSpec.Containers[i])
	}
	podSpec.Containers = containers
}

func (f *CollectorFunctionalFramework) deploySecrets() error {
	for _, secret := range f.Secrets {
		secret.Namespace = f.Namespace
		log.V(2).Info("Creating secret", "namespace", secret.Namespace, "name", secret.Name)
		if err := f.Test.Client.Create(secret); err != nil {
			return err
		}
	}
	return nil
}

func (f *CollectorFunctionalFramework) addOutputContainers(b *runtime.PodBuilder, outputs []logging.OutputSpec) error {
	log.V(2).Info("Adding outputs", "outputs", outputs)
	for _, output := range outputs {
		switch output.Type {
		case logging.OutputTypeFluentdForward:
			if err := f.AddForwardOutput(b, output); err != nil {
				return err
			}
		case logging.OutputTypeSyslog:
			if err := f.AddSyslogOutput(b, output); err != nil {
				return err
			}
		case logging.OutputTypeKafka:
			if err := f.AddKafkaOutput(b, output); err != nil {
				return err
			}
		case logging.OutputTypeElasticsearch:
			if err := f.AddES7Output(b, output); err != nil {
				return err
			}
		case logging.OutputTypeHttp:
			if err := f.AddFluentdHttpOutput(b, output); err != nil {
				return err
			}
		}
	}
	return nil
}

func (f *CollectorFunctionalFramework) WaitForPodToBeReady() error {
	return oc.Literal().From("oc wait -n %s pod/%s --timeout=60s --for=condition=Ready", f.Test.NS.Name, f.Name).Output()
}

func (f *CollectorFunctionalFramework) GetLogsFromCollector() (string, error) {
	output, err := oc.Literal().From("oc logs -n %s pod/%s -c %s", f.Test.NS.Name, f.Name, constants.CollectorName).Run()
	if err != nil {
		return output, err
	}
	return output, nil
}
