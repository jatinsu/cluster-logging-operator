package k8shandler

import (
  "k8s.io/apimachinery/pkg/api/errors"
  "github.com/sirupsen/logrus"
  "github.com/openshift/cluster-logging-operator/pkg/utils"
  "k8s.io/api/core/v1"

  sdk "github.com/operator-framework/operator-sdk/pkg/sdk"
  logging "github.com/openshift/cluster-logging-operator/pkg/apis/logging/v1alpha1"
)

func CreateOrUpdateCollection(logging *logging.ClusterLogging) error {
  createOrUpdateFluentdServiceAccount(logging)

  createOrUpdateCollectionPriorityClass(logging)
  createOrUpdateFluentdConfigMap(logging)
  createOrUpdateFluentdDaemonset(logging)

  return createOrUpdateFluentdSecret(logging)
}

func createOrUpdateCollectionPriorityClass(logging *logging.ClusterLogging) error {

  collectionPriorityClass := utils.PriorityClass("cluster-logging", 1000000, false, "This priority class is for the Cluster-Logging Collector")

  utils.AddOwnerRefToObject(collectionPriorityClass, utils.AsOwner(logging))

  err := sdk.Create(collectionPriorityClass)
  if err != nil && !errors.IsAlreadyExists(err) {
    logrus.Fatalf("Failure creating Collection priority class: %v", err)
  }

  return nil
}

func createOrUpdateFluentdServiceAccount(logging *logging.ClusterLogging) error {

  fluentdServiceAccount := utils.ServiceAccount("fluentd", logging.Namespace)

  utils.AddOwnerRefToObject(fluentdServiceAccount, utils.AsOwner(logging))

  err := sdk.Create(fluentdServiceAccount)
  if err != nil && !errors.IsAlreadyExists(err) {
    logrus.Fatalf("Failure creating Fluentd service account: %v", err)
  }

  return nil
}

func createOrUpdateFluentdConfigMap(logging *logging.ClusterLogging) error {

  fluentdConfigMap := utils.ConfigMap(
    "fluentd",
    logging.Namespace,
    map[string]string{
      "fluent.conf": string(utils.GetFileContents("files/fluent.conf")),
      "throttle-config.yaml": string(utils.GetFileContents("files/fluentd-throttle-config.yaml")),
      "secure-forward.conf": string(utils.GetFileContents("files/secure-forward.conf")),
    },
  )

  utils.AddOwnerRefToObject(fluentdConfigMap, utils.AsOwner(logging))

  err := sdk.Create(fluentdConfigMap)
  if err != nil && !errors.IsAlreadyExists(err) {
    logrus.Fatalf("Failure constructing Fluentd configmap: %v", err)
  }

  return nil
}

func createOrUpdateFluentdSecret(logging *logging.ClusterLogging) error {

  fluentdSecret := utils.Secret(
    "fluentd",
    logging.Namespace,
    map[string][]byte{
      "app-ca": utils.GetFileContents("/tmp/_working_dir/ca.crt"),
      "app-key": utils.GetFileContents("/tmp/_working_dir/system.logging.fluentd.key"),
      "app-cert": utils.GetFileContents("/tmp/_working_dir/system.logging.fluentd.crt"),
      "infra-ca": utils.GetFileContents("/tmp/_working_dir/ca.crt"),
      "infra-key": utils.GetFileContents("/tmp/_working_dir/system.logging.fluentd.key"),
      "infra-cert": utils.GetFileContents("/tmp/_working_dir/system.logging.fluentd.crt"),
    }  )

  utils.AddOwnerRefToObject(fluentdSecret, utils.AsOwner(logging))

  err := sdk.Create(fluentdSecret)
  if err != nil && !errors.IsAlreadyExists(err) {
    logrus.Fatalf("Failure constructing Fluentd secret: %v", err)
  }

  return nil
}

func createOrUpdateFluentdDaemonset(logging *logging.ClusterLogging) error {

  var fluentdPodSpec v1.PodSpec

  if utils.AllInOne(logging) {
    fluentdPodSpec = getFluentdPodSpec(logging, "elasticsearch", "elasticsearch")
  } else {
    fluentdPodSpec = getFluentdPodSpec(logging, "elasticsearch-app", "elasticsearch-infra")
  }

  fluentdDaemonset := utils.DaemonSet("fluentd", logging.Namespace, "fluentd", "fluentd", fluentdPodSpec)

  utils.AddOwnerRefToObject(fluentdDaemonset, utils.AsOwner(logging))

  err := sdk.Create(fluentdDaemonset)
  if err != nil && !errors.IsAlreadyExists(err) {
    logrus.Fatalf("Failure creating Fluentd Daemonset %v", err)
  }

  return nil
}

func getFluentdPodSpec(logging *logging.ClusterLogging, elasticsearchAppName string, elasticsearchInfraName string) v1.PodSpec {

  fluentdContainer := utils.Container("fluentd", v1.PullIfNotPresent, logging.Spec.Collection.FluentdSpec.Resources)

  fluentdContainer.Env = []v1.EnvVar{
    {Name: "MERGE_JSON_LOG", Value: "true"},
    {Name: "K8S_HOST_URL", Value: "https://kubernetes.default.svc"},
    {Name: "ES_HOST", Value: elasticsearchAppName},
    {Name: "ES_PORT", Value: "9200"},
    {Name: "ES_CLIENT_CERT", Value: "/etc/fluent/keys/app-cert"},
    {Name: "ES_CLIENT_KEY", Value: "/etc/fluent/keys/app-key"},
    {Name: "ES_CA", Value: "/etc/fluent/keys/app-ca"},
    {Name: "OPS_HOST", Value: elasticsearchInfraName},
    {Name: "OPS_PORT", Value: "9200"},
    {Name: "OPS_CLIENT_CERT", Value: "/etc/fluent/keys/infra-cert"},
    {Name: "OPS_CLIENT_KEY", Value: "/etc/fluent/keys/infra-key"},
    {Name: "OPS_CA", Value: "/etc/fluent/keys/infra-ca"},
    {Name: "JOURNAL_SOURCE", Value: ""},
    {Name: "JOURNAL_READ_FROM_HEAD", Value: ""},
    {Name: "BUFFER_QUEUE_LIMIT", Value: "32"},
    {Name: "BUFFER_SIZE_LIMIT", Value: "8m"},
    {Name: "FILE_BUFFER_LIMIT", Value: "256Mi"},
    {Name: "FLUENTD_CPU_LIMIT", ValueFrom: &v1.EnvVarSource{ResourceFieldRef: &v1.ResourceFieldSelector{ContainerName: "fluentd", Resource: "limits.cpu"}}},
    {Name: "FLUENTD_MEMORY_LIMIT", ValueFrom: &v1.EnvVarSource{ResourceFieldRef: &v1.ResourceFieldSelector{ContainerName: "fluentd", Resource: "limits.memory"}}},
  }

  fluentdContainer.VolumeMounts = []v1.VolumeMount{
    {Name: "runlogjournal", MountPath: "/run/log/journal"},
    {Name: "varlog", ReadOnly: true, MountPath: "/var/log"},
    {Name: "varlibdockercontainers", ReadOnly: true, MountPath: "/var/lib/docker"},
    {Name: "config", ReadOnly: true, MountPath: "/etc/fluent/configs.d/user"},
    {Name: "certs", ReadOnly: true, MountPath: "/etc/fluent/keys"},
    {Name: "dockerhostname", ReadOnly: true, MountPath: "/etc/docker-hostname"},
    {Name: "localtime", ReadOnly: true, MountPath: "/etc/localtime"},
    {Name: "dockercfg", ReadOnly: true, MountPath: "/etc/sysconfig/docker"},
    {Name: "dockerdaemoncfg", ReadOnly: true, MountPath: "/etc/docker"},
    {Name: "filebufferstorage", MountPath: "/var/lib/fluentd"},
  }

  fluentdContainer.SecurityContext = &v1.SecurityContext{
    Privileged: utils.GetBool(true),
  }

  fluentdPodSpec := utils.PodSpec(
    "fluentd",
    []v1.Container{fluentdContainer},
    []v1.Volume{
      {Name: "runlogjournal", VolumeSource: v1.VolumeSource{HostPath: &v1.HostPathVolumeSource{Path: "/run/log/journal"}}},
      {Name: "varlog", VolumeSource: v1.VolumeSource{HostPath: &v1.HostPathVolumeSource{Path: "/var/log"}}},
      {Name: "varlibdockercontainers", VolumeSource: v1.VolumeSource{HostPath: &v1.HostPathVolumeSource{Path: "/var/lib/docker"}}},
      {Name: "config", VolumeSource: v1.VolumeSource{ConfigMap: &v1.ConfigMapVolumeSource{LocalObjectReference: v1.LocalObjectReference{Name: "fluentd"}}}},
      {Name: "certs", VolumeSource: v1.VolumeSource{Secret: &v1.SecretVolumeSource{SecretName: "fluentd"}}},
      {Name: "dockerhostname", VolumeSource: v1.VolumeSource{HostPath: &v1.HostPathVolumeSource{Path: "/etc/hostname"}}},
      {Name: "localtime", VolumeSource: v1.VolumeSource{HostPath: &v1.HostPathVolumeSource{Path: "/etc/localtime"}}},
      {Name: "dockercfg", VolumeSource: v1.VolumeSource{HostPath: &v1.HostPathVolumeSource{Path: "/etc/sysconfig/docker"}}},
      {Name: "dockerdaemoncfg", VolumeSource: v1.VolumeSource{HostPath: &v1.HostPathVolumeSource{Path: "/etc/docker"}}},
      {Name: "filebufferstorage", VolumeSource: v1.VolumeSource{HostPath: &v1.HostPathVolumeSource{Path: "/var/lib/fluentd"}}},
    },
  )

  fluentdPodSpec.PriorityClassName = "cluster-logging"

  fluentdPodSpec.NodeSelector = map[string]string {
    "logging-infra-fluentd": "true",
  }

  return fluentdPodSpec
}
