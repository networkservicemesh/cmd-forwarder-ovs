// Copyright (c) 2022 Nordix Foundation.
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

// Package ovsinit provides functions to start ovs and check if ovs-vswitchd is running
package ovsinit

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/edwarnicke/exechelper"
	"github.com/pkg/errors"
)

const pidfile = "/var/run/openvswitch/ovs-vswitchd.pid"
const ovsVswitchdBin = "ovs-vswitchd"

// IsOvsRunning check the /proc for running ovs daemon
func IsOvsRunning() bool {
	files, err := os.ReadDir("/proc")
	if err != nil {
		return false
	}
	for _, file := range files {
		pid, err := strconv.ParseInt(file.Name(), 10, 0)
		if err == nil {
			if checkProc(int(pid)) {
				return true
			}
		}
	}
	return false
}

// StartSupervisord which run and supervise the ovs locally
func StartSupervisord(ctx context.Context) (errCh <-chan error) {
	progErrCh := exechelper.Start("/usr/bin/supervisord", exechelper.WithContext(ctx))
	select {
	case err := <-progErrCh:
		errCh := make(chan error, 1)
		errCh <- err
		close(errCh)
		return errCh
	default:
	}
	return progErrCh
}

// WaitForOvsVswitchd wait for ovs pid file in default directory and check if binary is running
func WaitForOvsVswitchd(timeout time.Duration) error {
	var err error
	var pid int
	ch := make(chan int, 1)
	go func() {
		for {
			p, errPid := getPidFromFile()
			if errPid == nil {
				ch <- p
				break
			}
		}
	}()

	select {
	case <-time.After(timeout * time.Second):
		err = errors.Errorf("timed out after %v", timeout*time.Second)
		return err
	case pid = <-ch:
	}
	if !checkProc(pid) {
		err = errors.Errorf("ovs-vswitchd failed to start")
		return err
	}
	return nil
}

func getPidFromFile() (int, error) {
	data, err := readData(pidfile)
	if err != nil {
		return 0, err
	}
	pid, err := strconv.Atoi(data)
	if err != nil {
		return 0, err
	}
	return pid, nil
}

func checkProc(p int) bool {
	commPath := fmt.Sprintf("/proc/%d/stat", p)
	data, err := readData(commPath)
	if err != nil {
		return false
	}
	// get the command name
	binStart := strings.IndexRune(data, '(') + 1
	binEnd := strings.IndexRune(data[binStart:], ')')
	binary := data[binStart : binStart+binEnd]

	if binary == ovsVswitchdBin && isRunning(data[binStart+binEnd+2:]) {
		return true
	}
	return false
}

func isRunning(data string) bool {
	var state rune
	_, err := fmt.Sscanf(data, "%c", &state)
	if err != nil {
		return false
	}
	// R  Running, S  Sleeping, D  Waiting
	if state == 'R' || state == 'S' || state == 'D' {
		return true
	}
	return false
}

func readData(commPath string) (string, error) {
	dataBytes, err := ioutil.ReadFile(filepath.Clean(commPath))
	if err != nil {
		return "", err
	}
	data := strings.TrimSpace(string(dataBytes))
	return data, nil
}
