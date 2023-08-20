package k8s

import (
	"fmt"
	"path/filepath"

	zen_targets "github.com/zen-io/zen-core/target"
	exec "github.com/zen-io/zen-target-exec"
	files "github.com/zen-io/zen-target-files"
)

type HelmChartConfig struct {
	Name        string            `mapstructure:"name" desc:"Name for the target"`
	Description string            `mapstructure:"desc" desc:"Target description"`
	Labels      []string          `mapstructure:"labels" desc:"Labels to apply to the targets"`
	Deps        []string          `mapstructure:"deps" desc:"Build dependencies"`
	PassEnv     []string          `mapstructure:"pass_env" desc:"List of environment variable names that will be passed from the OS environment, they are part of the target hash"`
	SecretEnv   []string          `mapstructure:"secret_env" desc:"List of environment variable names that will be passed from the OS environment, they are not used to calculate the target hash"`
	Env         map[string]string `mapstructure:"env" desc:"Key-Value map of static environment variables to be used"`
	Tools       map[string]string `mapstructure:"tools" desc:"Key-Value map of tools to include when executing this target. Values can be references"`
	Visibility  []string          `mapstructure:"visibility" desc:"List of visibility for this target"`
	Repo        *string           `mapstructure:"repo"`
	Chart       string            `mapstructure:"chart"`
	Version     *string           `mapstructure:"version"`
	Path        *string           `mapstructure:"path"`
	Toolchain   *string           `mapstructure:"toolchain"`
	Hashes      []string          `mapstructure:"hashes"`
}

func (hmc HelmChartConfig) GetTargets(tcc *zen_targets.TargetConfigContext) ([]*zen_targets.TargetBuilder, error) {
	if hmc.Path != nil {
		fc := files.FilegroupConfig{
			Srcs:            []string{fmt.Sprintf("%s/**/*", *hmc.Path)},
			Name:            hmc.Name,
			Labels:          hmc.Labels,
			Deps:            hmc.Deps,
			Visibility:      hmc.Visibility,
			NoCacheInterpolation: true,
		}
		return fc.GetTargets(tcc)
	} else {
		var toolchain string
		if hmc.Toolchain != nil {
			toolchain = *hmc.Toolchain
		} else if val, ok := tcc.KnownToolchains["helm"]; !ok {
			return nil, fmt.Errorf("helm toolchain is not configured")
		} else {
			toolchain = val
		}

		pullCmd := fmt.Sprintf("helm pull -d {CWD} --untar --version %s", *hmc.Version)
		if hmc.Repo != nil {
			pullCmd = fmt.Sprintf("%s --repo %s", pullCmd, *hmc.Repo)
		}

		hmc.Labels = append(hmc.Labels, fmt.Sprintf("chart=%s", hmc.Chart))
		if hmc.Version != nil {
			hmc.Labels = append(hmc.Labels, fmt.Sprintf("version=%s", *hmc.Version))
		}
		if hmc.Repo != nil {
			hmc.Labels = append(hmc.Labels, fmt.Sprintf("repo=%s", *hmc.Repo))
		}

		ec := exec.ExecConfig{
			Name:            hmc.Name,
			Labels:          hmc.Labels,
			Deps:            hmc.Deps,
			Visibility:      hmc.Visibility,
			NoCacheInterpolation: true,
			Outs:            []string{filepath.Base(hmc.Chart) + "/**/*"},
			BuildCommand:    []string{fmt.Sprintf("%s %s", pullCmd, hmc.Chart)},
			Tools:           map[string]string{"helm": toolchain},
		}

		return ec.GetTargets(tcc)
	}
}
