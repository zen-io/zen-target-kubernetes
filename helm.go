package k8s

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	ahoy_targets "gitlab.com/hidothealth/platform/ahoy/src/target"
	"gitlab.com/hidothealth/platform/ahoy/src/utils"
)

type HelmConfig struct {
	Srcs                      []string `mapstructure:"srcs"`
	DeployDeps                []string `mapstructure:"deploy_deps"`
	ValuesFiles               []string `mapstructure:"values_files"`
	Toolchain                 *string  `mapstructure:"toolchain"`
	ReleaseName               string   `mapstructure:"release_name"`
	Chart                     string   `mapstructure:"chart"`
	Version                   *string  `mapstructure:"version"`
	Namespace                 string   `mapstructure:"namespace"`
	ahoy_targets.BaseFields   `mapstructure:",squash"`
	ahoy_targets.DeployFields `mapstructure:",squash"`
}

func (hc HelmConfig) GetTargets(tcc *ahoy_targets.TargetConfigContext) ([]*ahoy_targets.Target, error) {
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

	if ahoy_targets.IsTargetReference(hc.Chart) {
		hc.Deps = append(hc.Deps, hc.Chart)
		srcs["chart"] = []string{hc.Chart}
		outs = append(outs, "chart/**/*")
	}

	return []*ahoy_targets.Target{
		ahoy_targets.NewTarget(
			hc.Name,
			ahoy_targets.WithSrcs(srcs),
			ahoy_targets.WithOuts(outs),
			ahoy_targets.WithEnvironments(hc.Environments),
			ahoy_targets.WithTools(map[string]string{"helm": toolchain}),
			ahoy_targets.WithPassEnv(hc.PassEnv),
			ahoy_targets.WithTargetScript("build", &ahoy_targets.TargetScript{
				Deps: hc.Deps,
				Run: func(target *ahoy_targets.Target, runCtx *ahoy_targets.RuntimeContext) error {
					for srcCat, sSrcs := range target.Srcs {
						if srcCat == "chart" {
							continue
						}

						for _, src := range sSrcs {
							if err := ahoy_targets.CopyWithInterpolate(src, src, target, runCtx); err != nil {
								return err
							}
						}
					}

					if ahoy_targets.IsTargetReference(hc.Chart) {
						for _, src := range target.Srcs["chart"] {
							to := filepath.Join(target.Cwd, "chart", strings.Join(strings.Split(src, "/")[1:], "/"))

							if err := utils.Copy(src, to); err != nil {
								return err
							}
						}
					}

					return nil
				},
			}),
			ahoy_targets.WithTargetScript("deploy", &ahoy_targets.TargetScript{
				Deps: hc.DeployDeps,
				Pre: func(target *ahoy_targets.Target, runCtx *ahoy_targets.RuntimeContext) error {
					args, err := valuesToArgs(hc, target, runCtx)
					if err != nil {
						return err
					}
					args = append(args, prepareCmdArgs(hc, target, runCtx)...)
					args = append(args, hc.ReleaseName, filepath.Join(target.Cwd, "chart"))
					if hc.Version != nil {
						args = append(args, "--version", *hc.Version)
					}

					target.Env["HELM_NAMESPACE"] = hc.Namespace
					target.Env["AHOY_DEBUG_CMD"] = fmt.Sprintf("helm upgrade -i %s", strings.Join(args, " "))
					return nil
				},
				Run: func(target *ahoy_targets.Target, runCtx *ahoy_targets.RuntimeContext) error {
					args, err := valuesToArgs(hc, target, runCtx)
					if err != nil {
						return err
					}
					args = append([]string{"upgrade", "-i", hc.ReleaseName, filepath.Join(target.Cwd, "chart")}, args...)
					args = append(args, prepareCmdArgs(hc, target, runCtx)...)
					if hc.Version != nil {
						args = append(args, "--version", *hc.Version)
					}

					helmCmd := exec.Command(target.Tools["helm"], args...)
					helmCmd.Dir = target.Cwd
					helmCmd.Env = target.GetEnvironmentVariablesList()
					helmCmd.Stdout = target
					helmCmd.Stderr = target
					if err := helmCmd.Run(); err != nil {
						return fmt.Errorf("executing deploy: %w", err)
					}

					return nil
				},
			}),
			ahoy_targets.WithTargetScript("remove", &ahoy_targets.TargetScript{
				Pre: func(target *ahoy_targets.Target, runCtx *ahoy_targets.RuntimeContext) error {
					target.Env["HELM_NAMESPACE"] = hc.Namespace
					target.Env["AHOY_DEBUG_CMD"] = "helm uninstall " + hc.ReleaseName
					return nil
				},
				Run: func(target *ahoy_targets.Target, runCtx *ahoy_targets.RuntimeContext) error {
					args := append([]string{"uninstall", hc.ReleaseName}, prepareCmdArgs(hc, target, runCtx)...)

					helmCmd := exec.Command(target.Tools["helm"], args...)
					helmCmd.Dir = target.Cwd
					helmCmd.Env = target.GetEnvironmentVariablesList()
					helmCmd.Stdout = target
					helmCmd.Stderr = target
					if err := helmCmd.Run(); err != nil {
						return fmt.Errorf("executing deploy: %w", err)
					}

					return nil
				},
			}),
		),
	}, nil
}

func prepareCmdArgs(hc HelmConfig, target *ahoy_targets.Target, runCtx *ahoy_targets.RuntimeContext) []string {
	args := []string{"--wait"}

	if runCtx.Debug {
		args = append(args, "--debug")
	}

	if runCtx.DryRun {
		args = append(args, "--dry-run")
	}

	return args
}

func valuesToArgs(hc HelmConfig, target *ahoy_targets.Target, runCtx *ahoy_targets.RuntimeContext) ([]string, error) {
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
