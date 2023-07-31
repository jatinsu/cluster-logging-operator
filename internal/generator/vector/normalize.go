package vector

import (
	"fmt"
	"strings"

	logging "github.com/openshift/cluster-logging-operator/apis/logging/v1"
	"github.com/openshift/cluster-logging-operator/internal/generator"
	. "github.com/openshift/cluster-logging-operator/internal/generator/vector/elements"
	"github.com/openshift/cluster-logging-operator/internal/generator/vector/helpers"
	. "github.com/openshift/cluster-logging-operator/internal/generator/vector/normalize"
)

const (
	FixLogLevel = `

if !exists(.level) {
	.level = "default"
	.severityNumber = "9"
	if match!(.message, r'Info|INFO|^I[0-9]+|level=info|Value:info|"level":"info"|<info>') {
	  .level = "info"
	  .severityNumber = "6"
	} else if match!(.message, r'Warning|WARN|^W[0-9]+|level=warn|Value:warn|"level":"warn"|<warn>') {
	  .level = "warn"
	  .severityNumber = "4"
	} else if match!(.message, r'Error|ERROR|^E[0-9]+|level=error|Value:error|"level":"error"|<error>') {
	  .level = "error"
	  .severityNumber = "3"
	} else if match!(.message, r'Critical|CRITICAL|^C[0-9]+|level=critical|Value:critical|"level":"critical"|<critical>') {
	  .level = "critical"
	  .severityNumber = "2"
	} else if match!(.message, r'Debug|DEBUG|^D[0-9]+|level=debug|Value:debug|"level":"debug"|<debug>') {
	  .level = "debug"
	  .severityNumber = "7"
	} else if match!(.message, r'Notice|NOTICE|^N[0-9]+|level=notice|Value:notice|"level":"notice"|<notice>') {
	  .level = "notice"
	  .severityNumber = "5"
	} else if match!(.message, r'Alert|ALERT|^A[0-9]+|level=alert|Value:alert|"level":"alert"|<alert>') {
	  .level = "alert"
	  .severityNumber = "1"
	} else if match!(.message, r'Emergency|EMERGENCY|^EM[0-9]+|level=emergency|Value:emergency|"level":"emergency"|<emergency>') {
	  .level = "emergency"
	  .severityNumber = "0"
	}
  }
.severityText = del(.level)
# resources
.resources.logs.file.path = del(.file)
.resources.host.name= del(.hostname)
.resources.container.name = del(.kubernetes.container_name)
.resources.container.id = del(.kubernetes.container_id)

# split image name and tag into separate fields
container_image_slice = split!(.kubernetes.container_image, ":", limit: 2)
.resources.container.image.name = container_image_slice[0]
.resources.container.image.tag = container_image_slice[1]
del(.kubernetes.container_image)

#kuberenetes
.resources.k8s.pod.name = del(.kubernetes.pod_name)
.resources.k8s.pod.uid = del(.kubernetes.pod_id)
.resources.k8s.pod.ip = del(.kubernetes.pod_ip)
.resources.k8s.pod.owner = .kubernetes.pod_owner
.resources.k8s.pod.annotations = del(.kubernetes.annotations)
.resources.k8s.pod.labels = del(.kubernetes.labels)
.resources.k8s.namespace.id = del(.kubernetes.namespace_id)

.resources.k8s.namespace.name = .kubernetes.namespace_labels."kubernetes.io/metadata.name"
.resources.k8s.namespace.labels = del(.kubernetes.namespace_labels)
.resources.attributes.key = "log_type"
.resources.attributes.value = del(.log_type)

`
	RemoveSourceType   = `del(.source_type)`
	RemoveStream       = `del(.stream)`
	RemovePodIPs       = `del(.kubernetes.pod_ips)`
	RemoveNodeLabels   = `del(.kubernetes.node_labels)`
	RemoveTimestampEnd = `del(.timestamp_end)`

	ParseHostAuditLogs = `
match1 = parse_regex(.message, r'type=(?P<type>[^ ]+)') ?? {}
envelop = {}
envelop |= {"type": match1.type}

match2, err = parse_regex(.message, r'msg=audit\((?P<ts_record>[^ ]+)\):')
if err == null {
  sp, err = split(match2.ts_record,":")
  if err == null && length(sp) == 2 {
      ts = to_unix_timestamp(to_timestamp!(del(.@timestamp))) ?? ""
      envelop |= {"record_id": sp[1]}
      . |= {"audit.linux" : envelop}
      . |= {"@timestamp" : format_timestamp(ts,"%+") ?? ""}
  }
} else {
  log("could not parse host audit msg. err=" + err, rate_limit_secs: 0)
}
`
	HostAuditLogTag = ".linux-audit.log"
	K8sAuditLogTag  = ".k8s-audit.log"
	OpenAuditLogTag = ".openshift-audit.log"
	OvnAuditLogTag  = ".ovn-audit.log"
	ParseAndFlatten = `. = merge(., parse_json!(string!(.message))) ?? .
del(.message)
`
	FixHostname = `.hostname = get_env_var("VECTOR_SELF_NODE_NAME") ?? ""`

	FixK8sAuditLevel       = `.k8s_audit_level = .level`
	FixOpenshiftAuditLevel = `.openshift_audit_level = .level`
	AddDefaultLogLevel     = `.level = "default"`
)

var (
	AddHostAuditTag = fmt.Sprintf(".tag = %q", HostAuditLogTag)
	AddK8sAuditTag  = fmt.Sprintf(".tag = %q", K8sAuditLogTag)
	AddOpenAuditTag = fmt.Sprintf(".tag = %q", OpenAuditLogTag)
	AddOvnAuditTag  = fmt.Sprintf(".tag = %q", OvnAuditLogTag)
)

func NormalizeLogs(spec *logging.ClusterLogForwarderSpec, op generator.Options) []generator.Element {
	types := generator.GatherSources(spec, op)
	var el []generator.Element = make([]generator.Element, 0)
	if types.Has(logging.InputNameApplication) || types.Has(logging.InputNameInfrastructure) {
		el = append(el, NormalizeContainerLogs("raw_container_logs", "container_logs")...)
	}
	if types.Has(logging.InputNameInfrastructure) {
		el = append(el, DropJournalDebugLogs("raw_journal_logs", "drop_journal_logs")...)
		el = append(el, JournalLogs("drop_journal_logs", "journal_logs")...)
	}
	if types.Has(logging.InputNameAudit) {
		el = append(el, NormalizeHostAuditLogs(RawHostAuditLogs, HostAuditLogs)...)
		el = append(el, NormalizeK8sAuditLogs(RawK8sAuditLogs, K8sAuditLogs)...)
		el = append(el, NormalizeOpenshiftAuditLogs(RawOpenshiftAuditLogs, OpenshiftAuditLogs)...)
		el = append(el, NormalizeOVNAuditLogs(RawOvnAuditLogs, OvnAuditLogs)...)
	}
	return el
}

func NormalizeContainerLogs(inLabel, outLabel string) []generator.Element {
	return []generator.Element{
		Remap{
			ComponentID: outLabel,
			Inputs:      helpers.MakeInputs(inLabel),
			VRL: strings.Join(helpers.TrimSpaces([]string{
				ClusterID,
				FixLogLevel,
				RemoveSourceType,
				RemoveStream,
				RemovePodIPs,
				RemoveNodeLabels,
				RemoveTimestampEnd,
				FixTimestampField,
			}), "\n"),
		},
	}
}

func NormalizeHostAuditLogs(inLabel, outLabel string) []generator.Element {
	return []generator.Element{
		Remap{
			ComponentID: outLabel,
			Inputs:      helpers.MakeInputs(inLabel),
			VRL: strings.Join(helpers.TrimSpaces([]string{
				ClusterID,
				AddHostAuditTag,
				ParseHostAuditLogs,
				AddDefaultLogLevel,
			}), "\n\n"),
		},
	}
}

func NormalizeK8sAuditLogs(inLabel, outLabel string) []generator.Element {
	return []generator.Element{
		Remap{
			ComponentID: outLabel,
			Inputs:      helpers.MakeInputs(inLabel),
			VRL: strings.Join(helpers.TrimSpaces([]string{
				ClusterID,
				AddK8sAuditTag,
				ParseAndFlatten,
				FixK8sAuditLevel,
				AddDefaultLogLevel,
			}), "\n"),
		},
	}
}

func NormalizeOpenshiftAuditLogs(inLabel, outLabel string) []generator.Element {
	return []generator.Element{
		Remap{
			ComponentID: outLabel,
			Inputs:      helpers.MakeInputs(inLabel),
			VRL: strings.Join(helpers.TrimSpaces([]string{
				ClusterID,
				AddOpenAuditTag,
				ParseAndFlatten,
				FixOpenshiftAuditLevel,
				AddDefaultLogLevel,
			}), "\n"),
		},
	}
}

func NormalizeOVNAuditLogs(inLabel, outLabel string) []generator.Element {
	return []generator.Element{
		Remap{
			ComponentID: outLabel,
			Inputs:      helpers.MakeInputs(inLabel),
			VRL: strings.Join(helpers.TrimSpaces([]string{
				ClusterID,
				AddOvnAuditTag,
				FixLogLevel,
			}), "\n"),
		},
	}
}
