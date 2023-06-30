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
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/edwarnicke/exechelper"
	"github.com/pkg/errors"
	"gopkg.in/fsnotify.v1"
)

const (
	defaultpath     = "/var/run/openvswitch/"
	vswitchdPidfile = "ovs-vswitchd.pid"
	dbPidfile       = "ovsdb-server.pid"
	vswitchdBinName = "ovs-vswitchd"
	dbBinName       = "ovsdb-server"
	dbSockfile      = "db.sock"
)

// IsOvsRunning check the /proc for running ovs daemon
func IsOvsRunning() bool {
	files, err := os.ReadDir("/proc")
	if err != nil {
		return false
	}
	for _, file := range files {
		pid, err := strconv.ParseInt(file.Name(), 10, 0)
		if err == nil {
			if checkProc(int(pid), vswitchdBinName) {
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

// WaitForOvs wait for ovs pid file in default directory and check if binary is running
func WaitForOvs(ctx context.Context, timeout time.Duration) error {
	vswitchdPidPath := filepath.Join(defaultpath, vswitchdPidfile)
	err := waitForOvsProc(ctx, timeout, vswitchdPidPath, vswitchdBinName)
	if err != nil {
		return err
	}
	dbPidPath := filepath.Join(defaultpath, dbPidfile)
	err = waitForOvsProc(ctx, timeout, dbPidPath, dbBinName)
	if err != nil {
		return err
	}
	dbSock := filepath.Join(defaultpath, dbSockfile)
	return waitForSocket(ctx, timeout, dbSock)
}

func waitForOvsProc(ctx context.Context, timeout time.Duration, pidFile, binName string) error {
	var err error
	var pid int
	ch := make(chan int, 1)
	go func() {
		for {
			p, errPid := getPidFromFile(pidFile)
			if errPid == nil {
				ch <- p
				break
			}
		}
	}()

	select {
	case pid = <-ch:
		if !checkProc(pid, binName) {
			err = errors.Errorf("%s failed to start", binName)
		}
	case <-time.After(timeout * time.Second):
		err = errors.Errorf("timed out after %v", timeout*time.Second)
	case <-ctx.Done():
		return errors.WithStack(ctx.Err())
	}
	return err
}

func waitForSocket(ctx context.Context, timeout time.Duration, dbSockName string) error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return errors.WithStack(err)
	}
	defer func() { _ = watcher.Close() }()
	if err = watcher.Add(filepath.Dir(dbSockName)); err != nil {
		return errors.WithStack(err)
	}

	_, err = os.Stat(dbSockName)
	if os.IsNotExist(err) {
		for {
			select {
			// watch for events
			case event := <-watcher.Events:
				if event.Name == dbSockName && event.Op == fsnotify.Create {
					return nil
				}
			// watch for errors
			case err = <-watcher.Errors:
				return errors.WithStack(err)
			case <-time.After(timeout * time.Second):
				return errors.Errorf("timed out after %v", timeout*time.Second)
			case <-ctx.Done():
				return errors.WithStack(ctx.Err())
			}
		}
	}
	if err != nil {
		return errors.WithStack(err)
	}
	return nil
}

func getPidFromFile(pidfile string) (int, error) {
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

func checkProc(p int, expected string) bool {
	commPath := fmt.Sprintf("/proc/%d/stat", p)
	data, err := readData(commPath)
	if err != nil {
		return false
	}
	// get the command name
	binStart := strings.IndexRune(data, '(') + 1
	binEnd := strings.IndexRune(data[binStart:], ')')
	binary := data[binStart : binStart+binEnd]

	if binary == expected && isRunning(data[binStart+binEnd+2:]) {
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
	dataBytes, err := os.ReadFile(filepath.Clean(commPath))
	if err != nil {
		return "", err
	}
	data := strings.TrimSpace(string(dataBytes))
	return data, nil
}
