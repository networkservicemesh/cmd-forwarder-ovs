// Copyright (c) 2021 Nordix Foundation.
//
// SPDX-License-Identifier: Apache-2.0
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at:
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package devicecfg provides service domain to device config
package devicecfg

import (
	"context"
	"fmt"
	"strings"

	"github.com/pkg/errors"

	"github.com/networkservicemesh/sdk-sriov/pkg/tools/yamlhelper"
	"github.com/networkservicemesh/sdk/pkg/tools/log/logruslogger"
)

// Config contains list of available service domains
type Config struct {
	Interfaces []*Resource `yaml:"interfaces"`
	Bridges    []*Resource `yaml:"bridges"`
}

func (c *Config) String() string {
	sb := &strings.Builder{}
	_, _ = sb.WriteString("&{")

	_, _ = sb.WriteString("Interfaces:[")
	var strs []string
	for _, device := range c.Interfaces {
		strs = append(strs, fmt.Sprintf("%+v", device))
	}

	_, _ = sb.WriteString(strings.Join(strs, " "))
	_, _ = sb.WriteString("]")

	_, _ = sb.WriteString(" Bridges:[")
	var strs1 []string
	for _, bridge := range c.Bridges {
		strs1 = append(strs1, fmt.Sprintf("%+v", bridge))
	}

	_, _ = sb.WriteString(strings.Join(strs1, " "))
	_, _ = sb.WriteString("]")

	_, _ = sb.WriteString("}")
	return sb.String()
}

// Resource contains an available interface or bridge name and related matches
type Resource struct {
	Name    string       `yaml:"name"`
	Matches []*Selectors `yaml:"matches"`
}

func (res *Resource) String() string {
	sb := &strings.Builder{}
	_, _ = sb.WriteString("&{")

	_, _ = sb.WriteString("Name:")
	_, _ = sb.WriteString(res.Name)

	_, _ = sb.WriteString(" Matches:[")
	var strs []string
	for _, selector := range res.Matches {
		strs = append(strs, fmt.Sprintf("%+v", selector))
	}

	_, _ = sb.WriteString(strings.Join(strs, " "))
	_, _ = sb.WriteString("]")

	_, _ = sb.WriteString("}")
	return sb.String()
}

// Selectors contains a list of selectors
type Selectors struct {
	LabelSelector []*Labels `yaml:"labelSelector"`
}

func (mh *Selectors) String() string {
	sb := &strings.Builder{}
	_, _ = sb.WriteString("&{")

	_, _ = sb.WriteString("LabelSelector[")
	var strs []string
	for _, labelSel := range mh.LabelSelector {
		strs = append(strs, fmt.Sprintf("%+v", labelSel))
	}
	_, _ = sb.WriteString(strings.Join(strs, " "))
	_, _ = sb.WriteString("]")

	_, _ = sb.WriteString("}")
	return sb.String()
}

// Labels contins the via selector
type Labels struct {
	Via string `yaml:"via"`
}

// ReadConfig reads configuration from file
func ReadConfig(ctx context.Context, configFile string) (*Config, error) {
	logger := logruslogger.New(ctx)

	cfg := &Config{}
	if err := yamlhelper.UnmarshalFile(configFile, cfg); err != nil {
		return nil, err
	}
	for _, device := range cfg.Interfaces {
		err := validateResource(device)
		if err != nil {
			return nil, err
		}
	}
	for _, bridge := range cfg.Bridges {
		err := validateResource(bridge)
		if err != nil {
			return nil, err
		}
	}
	logger.WithField("Config", "ReadConfig").Infof("unmarshalled Config: %+v", cfg)
	return cfg, nil
}

func validateResource(res *Resource) error {
	if res == nil {
		return nil
	}
	if res.Name == "" {
		return errors.Errorf("resource name must be set")
	}
	for i := range res.Matches {
		if len(res.Matches[i].LabelSelector) == 0 {
			return errors.Errorf("at least one label selector must be specified")
		}
		for j := range res.Matches[i].LabelSelector {
			if res.Matches[i].LabelSelector[j].Via == "" {
				return errors.Errorf("%s unsupported label selector specified", res.Name)
			}
		}
	}
	return nil
}
