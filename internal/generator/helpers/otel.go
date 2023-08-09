package helpers

import (
	"github.com/openshift/cluster-logging-operator/internal/generator"
	elements2 "github.com/openshift/cluster-logging-operator/internal/generator/fluentd/elements"
)

const (
	EnableOpenTelemetry = "otel"
)

func IsDebugOutputOtel(op generator.Options) bool {
	_, ok := op[EnableOpenTelemetry]
	return ok
}

var OtelOutput = generator.ConfLiteral{
	Desc:         "Adding otel output",
	TemplateName: "toStdout",
	Pattern:      "**",
	TemplateStr:  elements2.ToStdOut,
}
