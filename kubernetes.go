package k8s

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	environs "github.com/zen-io/zen-core/environments"
	zen_targets "github.com/zen-io/zen-core/target"
	"github.com/zen-io/zen-core/utils"
)

type KubernetesConfig struct {
	Name         string                           `mapstructure:"name" desc:"Name for the target"`
	Description  string                           `mapstructure:"desc" desc:"Target description"`
	Labels       []string                         `mapstructure:"labels" desc:"Labels to apply to the targets"`
	Deps         []string                         `mapstructure:"deps" desc:"Build dependencies"`
	PassEnv      []string                         `mapstructure:"pass_env" desc:"List of environment variable names that will be passed from the OS environment, they are part of the target hash"`
	SecretEnv    []string                         `mapstructure:"secret_env" desc:"List of environment variable names that will be passed from the OS environment, they are not used to calculate the target hash"`
	Env          map[string]string                `mapstructure:"env" desc:"Key-Value map of static environment variables to be used"`
	Tools        map[string]string                `mapstructure:"tools" desc:"Key-Value map of tools to include when executing this target. Values can be references"`
	Visibility   []string                         `mapstructure:"visibility" desc:"List of visibility for this target"`
	Environments map[string]*environs.Environment `mapstructure:"environments" desc:"Deployment Environments"`
	Srcs         []string                         `mapstructure:"srcs" desc:"Sources for the build"`
	Outs         []string                         `mapstructure:"outs" desc:"Outs for the build"`
	DeployDeps   []string                         `mapstructure:"deploy_deps"`
	Urls         []string                         `mapstructure:"urls"`
	Toolchain    *string                          `mapstructure:"toolchain" desc:"Kubectl executable. Can be a ref or path"`
	Namespace    *string                          `mapstructure:"namespace"`
}

func (kc KubernetesConfig) GetTargets(tcc *zen_targets.TargetConfigContext) ([]*zen_targets.Target, error) {
	srcs := map[string][]string{
		"all": {},
	}

	outs := []string{}

	for env := range kc.Environments {
		outs = append(outs, env)

		srcs[env] = []string{}
		interpolVars := map[string]string{"ENV": env}

		for _, src := range kc.Srcs {
			interpolated, err := tcc.Interpolate(src, interpolVars)
			if err != nil {
				return nil, fmt.Errorf("interpolating src file %s: %w", src, err)
			}

			if strings.Contains(src, "{ENV}") {
				srcs[env] = append(srcs[env], interpolated)
			} else {
				srcs["all"] = append(srcs[env], interpolated)
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

	labels := make([]string, 0)

	for _, url := range kc.Urls {
		if interpolated, err := tcc.Interpolate(url); err != nil {
			return nil, fmt.Errorf("interpolating url %s: %w", url, err)
		} else {
			labels = append(labels, fmt.Sprintf("url:%s", interpolated))
		}
	}

	opts := []zen_targets.TargetOption{
		zen_targets.WithSrcs(srcs),
		zen_targets.WithOuts(outs),
		zen_targets.WithVisibility(kc.Visibility),
		zen_targets.WithLabels(labels),
		zen_targets.WithEnvironments(kc.Environments),
		zen_targets.WithTools(map[string]string{"kubectl": toolchain}),
		zen_targets.WithEnvVars(map[string]string{"NAMESPACE": namespace}),
		zen_targets.WithTargetScript("build", &zen_targets.TargetScript{
			Deps: kc.Deps,
			Run: func(target *zen_targets.Target, runCtx *zen_targets.RuntimeContext) error {
				for env := range target.Environments {
					if err := os.MkdirAll(filepath.Join(target.Cwd, env), os.ModePerm); err != nil {
						return err
					}

					for _, src := range append(target.Srcs["all"], target.Srcs[env]...) {
						from := src
						to := filepath.Join(target.Cwd, env, filepath.Base(strings.TrimPrefix(src, target.Cwd)))

						if err := utils.CopyWithInterpolate(from, to, target.EnvVars()); err != nil {
							return fmt.Errorf("interpolating while copying from %s to %s: %w", from, to, err)
						}
					}
				}
				return nil
			},
		}),
		zen_targets.WithTargetScript("deploy", &zen_targets.TargetScript{
			Deps: kc.DeployDeps,
			Pre: func(target *zen_targets.Target, runCtx *zen_targets.RuntimeContext) error {
				args := []string{"apply", "--wait", "-f", runCtx.Env}
				if namespace != "" {
					args = append(args, "-n", namespace)
				}

				if runCtx.DryRun {
					args = append(args, "--dry-run", "server")
				}

				for _, label := range target.Labels {
					if strings.HasPrefix(label, "url:") {
						args = append(args, "-f", label[4:])
					}
				}

				target.Env["ZEN_DEBUG_CMD"] = fmt.Sprintf("%s %s", target.Tools["kubectl"], strings.Join(args, " "))
				return nil
			},
			Run: func(target *zen_targets.Target, runCtx *zen_targets.RuntimeContext) error {
				execEnv := target.GetEnvironmentVariablesList()

				args := []string{"apply", "--wait", "-f", runCtx.Env}
				if namespace != "" {
					args = append(args, "-n", namespace)
				}

				if runCtx.DryRun {
					args = append(args, "--dry-run", "server")
				}

				for _, label := range target.Labels {
					if strings.HasPrefix(label, "url:") {
						args = append(args, "-f", label[4:])
					}
				}

				kubeCmd := exec.Command(target.Tools["kubectl"], args...)
				kubeCmd.Dir = target.Cwd
				kubeCmd.Env = execEnv
				kubeCmd.Stdout = target
				kubeCmd.Stderr = target
				if err := kubeCmd.Run(); err != nil {
					return fmt.Errorf("executing deploy: %w", err)
				}

				return nil
			},
		}),
		zen_targets.WithTargetScript("remove", &zen_targets.TargetScript{
			Run: func(target *zen_targets.Target, runCtx *zen_targets.RuntimeContext) error {
				execEnv := target.GetEnvironmentVariablesList()

				args := []string{"remove", "--wait", "-n", namespace}

				if runCtx.DryRun {
					args = append(args, "--dry-run", "server")
				}

				for _, vf := range target.Srcs[fmt.Sprintf("values_%s", runCtx.Env)] {
					args = append(args, "-f", vf)
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

	return []*zen_targets.Target{
		zen_targets.NewTarget(
			kc.Name,
			opts...,
		),
	}, nil
}
