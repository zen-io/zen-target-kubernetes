package k8s

import (
	zen_targets "github.com/zen-io/zen-core/target"
)

var KnownTargets = zen_targets.TargetCreatorMap{
	"kubernetes": KubernetesConfig{},
	"helm":       HelmConfig{},
	"helm_chart": HelmChartConfig{},
}
