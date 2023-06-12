package k8s

import (
	ahoy_targets "gitlab.com/hidothealth/platform/ahoy/src/target"
)

var KnownTargets = ahoy_targets.TargetCreatorMap{
	"kubernetes": KubernetesConfig{},
	"helm":       HelmConfig{},
	"helm_chart": HelmChartConfig{},
}
