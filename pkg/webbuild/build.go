/* Copyright 2022 Zinc Labs Inc. and Contributors
*
* Licensed under the Apache License, Version 2.0 (the "License");
* you may not use this file except in compliance with the License.
* You may obtain a copy of the License at
*
*     http://www.apache.org/licenses/LICENSE-2.0
*
* Unless required by applicable law or agreed to in writing, software
* distributed under the License is distributed on an "AS IS" BASIS,
* WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
* See the License for the specific language governing permissions and
* limitations under the License.
 */

package webbuild

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/gofrs/flock"
)

// Build compiles the console and a self-contained web binary from a source checkout.
func Build() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	root, err := sourceRoot(dir)
	if err != nil {
		return "", err
	}
	return build(root, (*exec.Cmd).Run)
}

func sourceRoot(dir string) (string, error) {
	for {
		data, err := os.ReadFile(filepath.Join(dir, "go.mod"))
		if err != nil && !os.IsNotExist(err) {
			return "", err
		}
		fields := strings.Fields(string(data))
		if len(fields) >= 2 && fields[0] == "module" && fields[1] == "github.com/zincsearch/zincsearch" {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("web mode requires a ZincSearch source checkout; run from its root or cmd/zincsearch")
		}
		dir = parent
	}
}

// build serializes concurrent node launches from one checkout: npm ci wipes
// node_modules, and a running binary must be replaced by rename, not rewritten.
func build(root string, run func(*exec.Cmd) error) (_ string, err error) {
	dir := filepath.Join(root, "test")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	lock := flock.New(filepath.Join(dir, "zincsearch-web.lock"))
	if err := lock.Lock(); err != nil {
		return "", err
	}
	defer func() { err = errors.Join(err, lock.Unlock()) }()

	output := filepath.Join(dir, "zincsearch-web")
	temp := fmt.Sprintf("%s-%d", output, os.Getpid())
	if runtime.GOOS == "windows" {
		output += ".exe"
		temp += ".exe"
	}
	commands := []*exec.Cmd{
		npmCommand("ci"),
		npmCommand("run", "build"),
		exec.Command("go", "build", "-tags", "web", "-o", temp, "./cmd/zincsearch"),
	}
	commands[0].Dir = filepath.Join(root, "frontend")
	commands[1].Dir = commands[0].Dir
	commands[2].Dir = root
	for _, cmd := range commands {
		cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
		if err := run(cmd); err != nil {
			return "", fmt.Errorf("%s: %w", strings.Join(cmd.Args, " "), err)
		}
	}
	if err := os.Rename(temp, output); err != nil {
		// Windows cannot replace a binary another node is running; use this build's own copy.
		if runtime.GOOS == "windows" {
			return temp, nil
		}
		return "", err
	}
	return output, nil
}

func npmCommand(args ...string) *exec.Cmd {
	if runtime.GOOS == "windows" {
		return exec.Command("cmd", append([]string{"/c", "npm"}, args...)...)
	}
	return exec.Command("npm", args...)
}
