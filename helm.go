package k8s

import (
	"fmt"
	"path/filepath"
	"strings"

	environs "github.com/zen-io/zen-core/environments"
	zen_targets "github.com/zen-io/zen-core/target"
	"github.com/zen-io/zen-core/utils"
)

type HelmConfig struct {
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
	Srcs         []string                         `mapstructure:"srcs"`
	DeployDeps   []string                         `mapstructure:"deploy_deps"`
	ValuesFiles  []string                         `mapstructure:"values_files"`
	Toolchain    *string                          `mapstructure:"toolchain"`
	ReleaseName  string                           `mapstructure:"release_name"`
	Chart        string                           `mapstructure:"chart"`
	Version      *string                          `mapstructure:"version"`
	Namespace    string                           `mapstructure:"namespace"`
}

func (hc HelmConfig) GetTargets(tcc *zen_targets.TargetConfigContext) ([]*zen_targets.Target, error) {
	srcs := map[string][]string{
		"_srcs":  hc.Srcs,
		"values": {},
	}

	outs := hc.Srcs

	var toolchain string
	if hc.Toolchain != nil {
		toolchain = *hc.Toolchain
	} else if val, ok := tcc.KnownToolchains["helm"]; !ok {
		return nil, fmt.Errorf("helm toolchain is not configured")
	} else {
		toolchain = val
	}

	if zen_targets.IsTargetReference(hc.Chart) {
		hc.Deps = append(hc.Deps, hc.Chart)
		srcs["chart"] = []string{hc.Chart}
		outs = append(outs, "chart/**/*")
	}

	hc.Labels = append(hc.Labels,
		fmt.Sprintf("meta:release=%s", hc.ReleaseName),
		fmt.Sprintf("meta:namespace=%s", hc.Namespace),
		fmt.Sprintf("meta:chart=%s", hc.Chart),
	)

	if hc.Version != nil {
		hc.Labels = append(hc.Labels, fmt.Sprintf("meta:version=%s", *hc.Version))
	}

	return []*zen_targets.Target{
		zen_targets.NewTarget(
			hc.Name,
			zen_targets.WithSrcs(srcs),
			zen_targets.WithOuts(outs),
			zen_targets.WithLabels(hc.Labels),
			zen_targets.WithEnvironments(hc.Environments),
			zen_targets.WithTools(map[string]string{"helm": toolchain}),
			zen_targets.WithPassEnv(hc.PassEnv),
			zen_targets.WithTargetScript("build", &zen_targets.TargetScript{
				Deps: hc.Deps,
				Run: func(target *zen_targets.Target, runCtx *zen_targets.RuntimeContext) error {
					for srcCat, sSrcs := range target.Srcs {
						if srcCat == "chart" {
							continue
						}

						for _, src := range sSrcs {
							if err := utils.CopyWithInterpolate(src, src, target.EnvVars()); err != nil {
								return err
							}
						}
					}

					if zen_targets.IsTargetReference(hc.Chart) {
						for _, src := range target.Srcs["chart"] {
							to := filepath.Join(target.Cwd, "chart", strings.Join(strings.Split(target.StripCwd(src), "/")[1:], "/"))

							if err := utils.Copy(src, to); err != nil {
								return err
							}
						}
					}

					return nil
				},
			}),
			zen_targets.WithTargetScript("deploy", &zen_targets.TargetScript{
				Deps: hc.DeployDeps,
				Pre: func(target *zen_targets.Target, runCtx *zen_targets.RuntimeContext) error {
					args, err := valuesToArgs(hc, target, runCtx)
					if err != nil {
						return err
					}
					args = append(args, hc.ReleaseName)
					args = append(args, prepareCmdArgs(hc, target, runCtx)...)
					if zen_targets.IsTargetReference(hc.Chart) {
						args = append(args, filepath.Join(target.Cwd, "chart"))
					} else {
						args = append(args, hc.Chart)
					}
					if hc.Version != nil {
						args = append(args, "--version", *hc.Version)
					}

					target.Env["HELM_NAMESPACE"] = hc.Namespace
					target.Env["ZEN_DEBUG_CMD"] = fmt.Sprintf("helm upgrade -i %s", strings.Join(args, " "))
					return nil
				},
				Run: func(target *zen_targets.Target, runCtx *zen_targets.RuntimeContext) error {
					args, err := valuesToArgs(hc, target, runCtx)
					if err != nil {
						return err
					}
					args = append([]string{"upgrade", "-i", hc.ReleaseName}, args...)
					if zen_targets.IsTargetReference(hc.Chart) {
						args = append(args, filepath.Join(target.Cwd, "chart"))
					} else {
						args = append(args, hc.Chart)
					}
					args = append(args, prepareCmdArgs(hc, target, runCtx)...)
					if hc.Version != nil {
						args = append(args, "--version", *hc.Version)
					}

					return target.Exec(append([]string{target.Tools["helm"]}, args...), "helm deploy")
				},
			}),
			zen_targets.WithTargetScript("remove", &zen_targets.TargetScript{
				Pre: func(target *zen_targets.Target, runCtx *zen_targets.RuntimeContext) error {
					target.Env["HELM_NAMESPACE"] = hc.Namespace
					target.Env["ZEN_DEBUG_CMD"] = "helm uninstall " + hc.ReleaseName
					return nil
				},
				Run: func(target *zen_targets.Target, runCtx *zen_targets.RuntimeContext) error {
					args := append([]string{"uninstall", hc.ReleaseName}, prepareCmdArgs(hc, target, runCtx)...)

					return target.Exec(append([]string{target.Tools["helm"]}, args...), "helm remove")
				},
			}),
		),
	}, nil
}

func prepareCmdArgs(hc HelmConfig, target *zen_targets.Target, runCtx *zen_targets.RuntimeContext) []string {
	args := []string{"--wait"}

	if runCtx.Debug {
		args = append(args, "--debug")
	}

	if runCtx.DryRun {
		args = append(args, "--dry-run")
	}

	return args
}

func valuesToArgs(hc HelmConfig, target *zen_targets.Target, runCtx *zen_targets.RuntimeContext) ([]string, error) {
	args := []string{}
	for _, vf := range hc.ValuesFiles {
		interpolatedVarFile, err := target.Interpolate(vf)
		if err != nil {
			return nil, err
		}
		args = append(args, "-f", interpolatedVarFile)
	}

	return args, nil
}
