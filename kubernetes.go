package k8s

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	environs "github.com/zen-io/zen-core/environments"
	zen_targets "github.com/zen-io/zen-core/target"
	"github.com/zen-io/zen-core/utils"
)

type KubernetesConfig struct {
	Name                 string                           `mapstructure:"name" zen:"yes" desc:"Name for the target"`
	Description          string                           `mapstructure:"desc" zen:"yes" desc:"Target description"`
	Labels               []string                         `mapstructure:"labels" zen:"yes" desc:"Labels to apply to the targets"`
	Deps                 []string                         `mapstructure:"deps" zen:"yes"  desc:"Build dependencies"`
	PassEnv              []string                         `mapstructure:"pass_env" zen:"yes" desc:"List of environment variable names that will be passed from the OS environment, they are part of the target hash"`
	PassSecretEnv        []string                         `mapstructure:"secret_env" zen:"yes" desc:"List of environment variable names that will be passed from the OS environment, they are not used to calculate the target hash"`
	Env                  map[string]string                `mapstructure:"env" zen:"yes" desc:"Key-Value map of static environment variables to be used"`
	Tools                map[string]string                `mapstructure:"tools" zen:"yes" desc:"Key-Value map of tools to include when executing this target. Values can be references"`
	Visibility           []string                         `mapstructure:"visibility" zen:"yes" desc:"List of visibility for this target"`
	Environments         map[string]*environs.Environment `mapstructure:"environments" zen:"yes" desc:"Deployment Environments"`
	Srcs                 []string                         `mapstructure:"srcs" desc:"Sources for the build"`
	Apply                []string                         `mapstructure:"apply"`
	Outs                 []string                         `mapstructure:"outs" desc:"Outs for the build"`
	DeployDeps           []string                         `mapstructure:"deploy_deps"`
	Urls                 []string                         `mapstructure:"urls"`
	NoCacheInterpolation bool                             `mapstructure:"no_interpolation" zen:"yes" desc:"Disable content interpolation"`
	Toolchain            *string                          `mapstructure:"toolchain" desc:"Kubectl executable. Can be a ref or path"`
	Namespace            *string                          `mapstructure:"namespace"`
}

func (kc KubernetesConfig) GetTargets(tcc *zen_targets.TargetConfigContext) ([]*zen_targets.TargetBuilder, error) {
	// srcs := map[string][]string{
	// 	"all": {},
	// }

	outs := []string{}

	for env := range kc.Environments {
		outs = append(outs, env)

		// srcs[env] = []string{}
		// interpolVars := map[string]string{"DEPLOY_ENV": env}

		// for _, src := range kc.Srcs {
		// 	interpolated, err := tcc.Interpolate(src, interpolVars)
		// 	if err != nil {
		// 		return nil, fmt.Errorf("interpolating src file %s: %w", src, err)
		// 	}

		// 	srcs[env] = append(srcs[env], interpolated)
		// }
	}

	kc.Labels = append(kc.Labels, fmt.Sprintf("apply=%s", strings.Join(kc.Apply, ",")))

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

	for _, url := range kc.Urls {
		if interpolated, err := tcc.Interpolate(url); err != nil {
			return nil, fmt.Errorf("interpolating url %s: %w", url, err)
		} else {
			kc.Labels = append(kc.Labels, fmt.Sprintf("url:%s", interpolated))
		}
	}

	t := zen_targets.ToTarget(kc)
	t.Srcs = map[string][]string{"_srcs": kc.Srcs}
	t.Outs = outs
	t.Tools = utils.MergeMaps(kc.Tools, map[string]string{"kubectl": toolchain})
	t.Env["NAMESPACE"] = namespace

	t.Scripts["build"] = &zen_targets.TargetBuilderScript{
		Deps: kc.Deps,
		Run: func(target *zen_targets.Target, runCtx *zen_targets.RuntimeContext) error {
			var env_label, apply_label string
			for _, l := range target.Labels {
				if strings.HasPrefix(l, "environments=") {
					env_label = strings.TrimPrefix(l, "environments=")
				} else if strings.HasPrefix(l, "apply=") {
					apply_label = strings.TrimPrefix(l, "apply=")
				}
			}

			for _, env := range strings.Split(env_label, ",") {
				if err := os.MkdirAll(filepath.Join(target.Cwd, env), os.ModePerm); err != nil {
					return err
				}

				for _, src := range strings.Split(apply_label, ",") {
					apply_file, err := target.Interpolate(src, map[string]string{"DEPLOY_ENV": env})
					if err != nil {
						return fmt.Errorf("interpolating apply file %s: %w", src, err)
					}

					from := filepath.Join(target.Cwd, apply_file)
					to := filepath.Join(target.Cwd, env, filepath.Base(apply_file))

					if err := target.Copy(from, to, target.Env, map[string]string{"DEPLOY_ENV": env}); err != nil {
						return fmt.Errorf("building env %s: %w", env, err)
					}
				}
			}

			return nil
		},
	}

	t.Scripts["deploy"] = &zen_targets.TargetBuilderScript{
		Deps: kc.DeployDeps,
		Pre: func(target *zen_targets.Target, runCtx *zen_targets.RuntimeContext) error {
			target.Env["ZEN_DEBUG_CMD"] = strings.Join(createArgs("apply", target, runCtx), " ")
			return nil
		},
		Run: func(target *zen_targets.Target, runCtx *zen_targets.RuntimeContext) error {
			target.SetStatus(fmt.Sprintf("Deploying %s", target.Qn()))
			return target.Exec(strings.Split(target.Env["ZEN_DEBUG_CMD"], " "), "kube apply")
		},
	}

	t.Scripts["remove"] = &zen_targets.TargetBuilderScript{
		Pre: func(target *zen_targets.Target, runCtx *zen_targets.RuntimeContext) error {
			target.Env["ZEN_DEBUG_CMD"] = strings.Join(createArgs("delete", target, runCtx), " ")
			return nil
		},
		Run: func(target *zen_targets.Target, runCtx *zen_targets.RuntimeContext) error {
			return target.Exec(strings.Split(target.Env["ZEN_DEBUG_CMD"], " "), "kube delete")
		},
	}

	return []*zen_targets.TargetBuilder{t}, nil
}

func createArgs(cmd string, target *zen_targets.Target, ctx *zen_targets.RuntimeContext) []string {
	args := []string{target.Tools["kubectl"], cmd, "--wait", "-f", ctx.Env}
	if target.Env["NAMESPACE"] != "" {
		args = append(args, "-n", target.Env["NAMESPACE"])
	}

	if ctx.DryRun {
		args = append(args, "--dry-run=server")
	}

	for _, label := range target.Labels {
		if strings.HasPrefix(label, "url:") {
			args = append(args, "-f", label[4:])
		}
	}

	return args
}
