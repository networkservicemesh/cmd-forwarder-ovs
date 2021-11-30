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

package l2resourcecfg_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	devicecfg "github.com/networkservicemesh/cmd-forwarder-ovs/internal/l2resourcecfg"
)

const (
	configFileName = "config.yml"
	bridgeName     = "br-data"
	ifName1        = "eth1"
	ifBridge       = "br0"
	via0           = "gw0"
	via1           = "gw1"
)

func TestReadConfigFile(t *testing.T) {
	cfg, err := devicecfg.ReadConfig(context.Background(), configFileName)
	require.NoError(t, err)
	require.Equal(t, &devicecfg.Config{
		Interfaces: []*devicecfg.Resource{
			{
				Name:   ifName1,
				Bridge: ifBridge,
				Matches: []*devicecfg.Selectors{
					{
						LabelSelector: []*devicecfg.Labels{
							{
								Via: via0,
							},
						},
					},
				},
			},
		},
		Bridges: []*devicecfg.Resource{
			{
				Name: bridgeName,
				Matches: []*devicecfg.Selectors{
					{
						LabelSelector: []*devicecfg.Labels{
							{
								Via: via1,
							},
						},
					},
				},
			},
		},
	}, cfg)
}
