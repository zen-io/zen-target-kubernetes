package k8s

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	ahoy_targets "gitlab.com/hidothealth/platform/ahoy/src/target"
)

type KubernetesConfig struct {
	DeployDeps                []string `mapstructure:"deploy_deps"`
	Toolchain                 *string  `mapstructure:"toolchain" desc:"Kubectl executable. Can be a ref or path"`
	Namespace                 *string  `mapstructure:"namespace"`
	ahoy_targets.BuildFields  `mapstructure:",squash"`
	ahoy_targets.DeployFields `mapstructure:",squash"`
}

func (kc KubernetesConfig) GetTargets(tcc *ahoy_targets.TargetConfigContext) ([]*ahoy_targets.Target, error) {
	srcs := map[string][]string{
		"srcs": {},
	}

	outs := []string{}

	for env := range kc.Environments {
		interpolVars := map[string]string{"ENV": env}

		srcName := fmt.Sprintf("values_%s", env)
		srcs[srcName] = []string{}
		for _, src := range kc.Srcs {
			if interpolated, err := tcc.Interpolate(src, interpolVars); err != nil {
				return nil, fmt.Errorf("interpolating src file %s: %w", src, err)
			} else {
				srcs[srcName] = append(srcs[srcName], interpolated)
				outs = append(outs, filepath.Join(env, filepath.Base(interpolated)))
			}
		}
	}

	var toolchain string
	if kc.Toolchain != nil {
		toolchain = *kc.Toolchain
	} else if val, ok := tcc.KnownToolchains["kubectl"]; !ok {
		return nil, fmt.Errorf("kubectl toolchain is not configured")
	} else {
		toolchain = val
	}

	var namespace string
	if kc.Namespace != nil {
		namespace = *kc.Namespace
	} else {
		namespace = ""
	}

	opts := []ahoy_targets.TargetOption{
		ahoy_targets.WithSrcs(srcs),
		ahoy_targets.WithOuts(outs),
		ahoy_targets.WithVisibility(kc.Visibility),
		ahoy_targets.WithLabels(kc.Labels),
		ahoy_targets.WithEnvironments(kc.Environments),
		ahoy_targets.WithTools(map[string]string{"kubectl": toolchain}),
		ahoy_targets.WithEnvVars(map[string]string{"NAMESPACE": namespace}),
		ahoy_targets.WithTargetScript("build", &ahoy_targets.TargetScript{
			Deps: kc.Deps,
			Run: func(target *ahoy_targets.Target, runCtx *ahoy_targets.RuntimeContext) error {
				for env := range target.Environments {
					for _, sSrcs := range target.Srcs {
						for _, src := range sSrcs {
							from := src
							to := filepath.Join(target.Cwd, env, filepath.Base(strings.TrimPrefix(src, target.Cwd)))

							if err := ahoy_targets.CopyWithInterpolate(from, to, target, runCtx); err != nil {
								return fmt.Errorf("interpolating while copying from %s to %s: %w", from, to, err)
							}
						}
					}
				}
				return nil
			},
		}),
		ahoy_targets.WithTargetScript("deploy", &ahoy_targets.TargetScript{
			Deps: kc.DeployDeps,
			Run: func(target *ahoy_targets.Target, runCtx *ahoy_targets.RuntimeContext) error {
				execEnv := target.GetEnvironmentVariablesList()

				args := []string{"apply", "--wait", "-n", namespace}

				if runCtx.DryRun {
					args = append(args, "--dry-run", "server")
				}

				for _, vf := range target.Srcs[fmt.Sprintf("values_%s", runCtx.Env)] {
					args = append(args, "-f", filepath.Base(vf))
				}

				kubeCmd := exec.Command(target.Tools["kubectl"], args...)
				kubeCmd.Dir = fmt.Sprintf("%s/%s", target.Cwd, runCtx.Env)
				kubeCmd.Env = execEnv
				kubeCmd.Stdout = target
				kubeCmd.Stderr = target
				if err := kubeCmd.Run(); err != nil {
					return fmt.Errorf("executing deploy: %w", err)
				}

				return nil
			},
		}),
		ahoy_targets.WithTargetScript("remove", &ahoy_targets.TargetScript{
			Run: func(target *ahoy_targets.Target, runCtx *ahoy_targets.RuntimeContext) error {
				execEnv := target.GetEnvironmentVariablesList()

				args := []string{"remove", "--wait", "-n", namespace}

				if runCtx.DryRun {
					args = append(args, "--dry-run", "server")
				}

				for _, vf := range target.Srcs[fmt.Sprintf("values_%s", runCtx.Env)] {
					args = append(args, "-f", filepath.Base(vf))
				}

				kubeCmd := exec.Command(target.Tools["kubectl"], args...)
				kubeCmd.Dir = fmt.Sprintf("%s/%s", target.Cwd, runCtx.Env)
				kubeCmd.Env = execEnv
				kubeCmd.Stdout = target
				kubeCmd.Stderr = target
				if err := kubeCmd.Run(); err != nil {
					return fmt.Errorf("executing deploy: %w", err)
				}

				return nil
			},
		}),
	}

	return []*ahoy_targets.Target{
		ahoy_targets.NewTarget(
			kc.Name,
			opts...,
		),
	}, nil
}
