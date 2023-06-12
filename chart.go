package k8s

import (
	"fmt"
	"path/filepath"

	exec "github.com/tiagoposse/ahoy-exec"
	files "github.com/tiagoposse/ahoy-files"
	ahoy_targets "gitlab.com/hidothealth/platform/ahoy/src/target"
)

type HelmChartConfig struct {
	ahoy_targets.BaseFields `mapstructure:",squash"`
	Repo                    *string  `mapstructure:"repo"`
	Chart                   string   `mapstructure:"chart"`
	Version                 *string  `mapstructure:"version"`
	Path                    *string  `mapstructure:"path"`
	Toolchain               *string  `mapstructure:"toolchain"`
	Hashes                  []string `mapstructure:"hashes"`
}

func (hmc HelmChartConfig) GetTargets(tcc *ahoy_targets.TargetConfigContext) ([]*ahoy_targets.Target, error) {
	if hmc.Path != nil {
		fc := files.FilegroupConfig{
			BuildFields: ahoy_targets.BuildFields{
				Srcs: []string{fmt.Sprintf("%s/**/*", *hmc.Path)},
				BaseFields: ahoy_targets.BaseFields{
					Name:       hmc.BaseFields.Name,
					Labels:     hmc.BaseFields.Labels,
					Deps:       hmc.BaseFields.Deps,
					Visibility: hmc.BaseFields.Visibility,
				},
			},
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

		ec := exec.ExecConfig{
			BuildFields: ahoy_targets.BuildFields{
				BaseFields: ahoy_targets.BaseFields{
					Name:       hmc.BaseFields.Name,
					Labels:     hmc.BaseFields.Labels,
					Deps:       hmc.BaseFields.Deps,
					Visibility: hmc.BaseFields.Visibility,
				},
			},
			Outs:         []string{filepath.Base(hmc.Chart) + "/**/*"},
			BuildCommand: []string{fmt.Sprintf("%s %s", pullCmd, hmc.Chart)},
			Tools:        map[string]string{"helm": toolchain},
		}

		return ec.GetTargets(tcc)
	}
}
