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

type HelmConfig struct {
	Name          string                           `mapstructure:"name" zen:"yes" desc:"Name for the target"`
	Description   string                           `mapstructure:"desc" zen:"yes" desc:"Target description"`
	Labels        []string                         `mapstructure:"labels" zen:"yes" desc:"Labels to apply to the targets"`
	Deps          []string                         `mapstructure:"deps" zen:"yes" desc:"Build dependencies"`
	PassEnv       []string                         `mapstructure:"pass_env" zen:"yes" desc:"List of environment variable names that will be passed from the OS environment, they are part of the target hash"`
	PassSecretEnv []string                         `mapstructure:"secret_env" zen:"yes" desc:"List of environment variable names that will be passed from the OS environment, they are not used to calculate the target hash"`
	Env           map[string]string                `mapstructure:"env" zen:"yes" desc:"Key-Value map of static environment variables to be used"`
	Tools         map[string]string                `mapstructure:"tools" zen:"yes" desc:"Key-Value map of tools to include when executing this target. Values can be references"`
	Visibility    []string                         `mapstructure:"visibility" zen:"yes" desc:"List of visibility for this target"`
	Environments  map[string]*environs.Environment `mapstructure:"environments" zen:"yes" desc:"Deployment Environments"`
	Args          map[string]string                `mapstructure:"args"`
	Srcs          []string                         `mapstructure:"srcs"`
	DeployDeps    []string                         `mapstructure:"deploy_deps"`
	ValuesFiles   []string                         `mapstructure:"values_files"`
	Toolchain     *string                          `mapstructure:"toolchain"`
	ReleaseName   string                           `mapstructure:"release_name"`
	Chart         string                           `mapstructure:"chart"`
	Version       *string                          `mapstructure:"version"`
	Namespace     string                           `mapstructure:"namespace"`
}

func (hc HelmConfig) GetTargets(tcc *zen_targets.TargetConfigContext) ([]*zen_targets.TargetBuilder, error) {
	srcs := map[string][]string{
		"_srcs":  hc.Srcs,
		"values": {},
	}

	outs := []string{"out/**"}

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

	if hc.Args != nil {
		for k, v := range hc.Args {
			hc.Labels = append(hc.Labels, fmt.Sprintf("arg=%s=%s", k, v))
		}
	}

	if hc.Version != nil {
		hc.Labels = append(hc.Labels, fmt.Sprintf("meta:version=%s", *hc.Version))
	}

	t := zen_targets.ToTarget(hc)
	t.Srcs = srcs
	t.Outs = outs
	t.Tools["helm"] = toolchain

	t.Scripts["build"] = &zen_targets.TargetBuilderScript{
		Deps: hc.Deps,
		Run: func(target *zen_targets.Target, runCtx *zen_targets.RuntimeContext) error {
			os.MkdirAll(filepath.Join(target.Cwd, "out"), os.ModePerm)
			for srcCat, sSrcs := range target.Srcs {
				if srcCat == "chart" {
					continue
				}

				for _, src := range sSrcs {
					to := filepath.Join(target.Cwd, "out", target.StripCwd(src))

					if err := target.Copy(src, to); err != nil {
						return err
					}
				}
			}

			if zen_targets.IsTargetReference(hc.Chart) {
				for _, src := range target.Srcs["chart"] {
					to := filepath.Join(target.Cwd, "chart", strings.Join(strings.Split(target.StripCwd(src), "/")[1:], "/"))

					if err := utils.Link(src, to); err != nil {
						return err
					}
				}
			}

			return nil
		},
	}

	t.Scripts["deploy"] = &zen_targets.TargetBuilderScript{
		Deps: hc.DeployDeps,
		Pre: func(target *zen_targets.Target, runCtx *zen_targets.RuntimeContext) error {
			target.SetStatus(fmt.Sprintf("Deploying %s", target.Qn()))

			args := []string{target.Tools["helm"], "upgrade", "-i"}
			args = append(args, prepareCmdArgs(hc, runCtx)...)
			args = append(args, hc.ReleaseName)

			for _, vf := range hc.ValuesFiles {
				interpolatedVarFile, err := target.Interpolate(vf)
				if err != nil {
					return err
				}
				args = append(args, "-f", filepath.Join("out", interpolatedVarFile))
			}

			if zen_targets.IsTargetReference(hc.Chart) {
				args = append(args, filepath.Join(target.Cwd, "chart"))
			} else if strings.HasPrefix(hc.Chart, "http") {
				chart := filepath.Base(hc.Chart)
				repo := strings.TrimSuffix(hc.Chart, "/"+chart)
				args = append(args, fmt.Sprintf("--repo=%s", repo), chart)
			} else {
				args = append(args, hc.Chart)
			}

			if hc.Version != nil {
				args = append(args, "--version", *hc.Version)
			}

			for _, label := range target.Labels {
				if strings.HasPrefix(label, "arg=") {
					argVal := strings.TrimPrefix(label, "arg=")
					interpolatedArg, err := target.Interpolate(argVal)
					if err != nil {
						return fmt.Errorf("interpolating arg %s: %w", argVal, err)
					}

					args = append(args, fmt.Sprintf("--set %s", interpolatedArg))
				}
			}

			target.Env["HELM_NAMESPACE"] = hc.Namespace
			target.Env["ZEN_DEBUG_CMD"] = strings.Join(args, " ")
			return nil
		},
		Run: func(target *zen_targets.Target, runCtx *zen_targets.RuntimeContext) error {
			return target.Exec(strings.Split(target.Env["ZEN_DEBUG_CMD"], " "), "helm deploy")
		},
	}
	t.Scripts["remove"] = &zen_targets.TargetBuilderScript{
		Pre: func(target *zen_targets.Target, runCtx *zen_targets.RuntimeContext) error {
			target.Env["HELM_NAMESPACE"] = hc.Namespace
			args := []string{target.Tools["helm"], "uninstall", hc.ReleaseName}
			args = append(args, prepareCmdArgs(hc, runCtx)...)
			target.Env["ZEN_DEBUG_CMD"] = strings.Join(args, " ")
			return nil
		},
		Run: func(target *zen_targets.Target, runCtx *zen_targets.RuntimeContext) error {
			return target.Exec(strings.Split(target.Env["ZEN_DEBUG_CMD"], " "), "helm remove")
		},
	}

	return []*zen_targets.TargetBuilder{t}, nil
}

func prepareCmdArgs(hc HelmConfig, runCtx *zen_targets.RuntimeContext) []string {
	args := []string{"--wait"}

	// if runCtx.Debug {
	// 	args = append(args, "--debug")
	// }

	if runCtx.DryRun {
		args = append(args, "--dry-run")
	}

	return args
}
