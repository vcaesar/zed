/* Copyright 2026 AtomAI
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
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSourceRoot(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "go.mod"), []byte("module github.com/zincsearch/zincsearch\n"), 0o600))
	for _, path := range []string{".", "cmd/zincsearch", "distribution/cmd"} {
		t.Run(path, func(t *testing.T) {
			dir := filepath.Join(root, path)
			require.NoError(t, os.MkdirAll(dir, 0o755))
			if path == "distribution/cmd" {
				require.NoError(t, os.WriteFile(filepath.Join(root, "distribution/go.mod"), []byte("module github.com/zincsearch/zincsearch/dist\n"), 0o600))
			}
			got, err := sourceRoot(dir)
			require.NoError(t, err)
			assert.Equal(t, root, got)
		})
	}
	t.Run("outside checkout", func(t *testing.T) {
		_, err := sourceRoot(filepath.VolumeName(root) + string(filepath.Separator))
		require.ErrorContains(t, err, "source checkout")
	})
	t.Run("unreadable module", func(t *testing.T) {
		dir := filepath.Join(root, "invalid")
		require.NoError(t, os.MkdirAll(filepath.Join(dir, "go.mod"), 0o755))
		_, err := sourceRoot(dir)
		require.Error(t, err)
	})
}

func TestBuild(t *testing.T) {
	for _, failAt := range []int{-1, 0, 1, 2} {
		t.Run(fmt.Sprintf("failure_%d", failAt), func(t *testing.T) {
			root := t.TempDir()
			output := filepath.Join(root, "test/zincsearch-web")
			temp := fmt.Sprintf("%s-%d", output, os.Getpid())
			if runtime.GOOS == "windows" {
				output += ".exe"
				temp += ".exe"
			}
			failure := errors.New("command failed")
			var commands []*exec.Cmd
			binary, err := build(root, func(cmd *exec.Cmd) error {
				commands = append(commands, cmd)
				if len(commands)-1 == failAt {
					return failure
				}
				return fakeGoBuild(cmd)
			})
			if failAt >= 0 {
				require.ErrorIs(t, err, failure)
				assert.ErrorContains(t, err, commands[failAt].Args[0])
				assert.Empty(t, binary)
				require.Len(t, commands, failAt+1)
			} else {
				require.NoError(t, err)
				assert.Equal(t, output, binary)
				assert.FileExists(t, output)
				assert.NoFileExists(t, temp)
				require.Len(t, commands, 3)
			}
			want := [][]string{
				{"npm", "ci"},
				{"npm", "run", "build"},
				{"go", "build", "-tags", "web", "-o", temp, "./cmd/zincsearch"},
			}
			if runtime.GOOS == "windows" {
				want[0] = append([]string{"cmd", "/c"}, want[0]...)
				want[1] = append([]string{"cmd", "/c"}, want[1]...)
			}
			for i, cmd := range commands {
				assert.Equal(t, want[i], cmd.Args)
				dir := root
				if i < 2 {
					dir = filepath.Join(root, "frontend")
				}
				assert.Equal(t, dir, cmd.Dir)
				assert.Equal(t, os.Stdin, cmd.Stdin)
				assert.Equal(t, os.Stdout, cmd.Stdout)
				assert.Equal(t, os.Stderr, cmd.Stderr)
			}
		})
	}
	t.Run("output directory error", func(t *testing.T) {
		root := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(root, "test"), nil, 0o600))
		_, err := build(root, func(*exec.Cmd) error {
			t.Fatal("must not run a command")
			return nil
		})
		require.Error(t, err)
	})
	t.Run("replace error", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("windows falls back to the per-process binary")
		}
		root := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(root, "test/zincsearch-web/busy"), 0o755))
		binary, err := build(root, fakeGoBuild)
		require.Error(t, err)
		assert.Empty(t, binary)
	})
	t.Run("concurrent nodes are serialized", func(t *testing.T) {
		root := t.TempDir()
		var active, peak atomic.Int32
		var wg sync.WaitGroup
		errs := make([]error, 3)
		for i := range errs {
			wg.Go(func() {
				_, errs[i] = build(root, func(cmd *exec.Cmd) error {
					n := active.Add(1)
					defer active.Add(-1)
					for p := peak.Load(); n > p && !peak.CompareAndSwap(p, n); p = peak.Load() {
					}
					time.Sleep(10 * time.Millisecond)
					return fakeGoBuild(cmd)
				})
			})
		}
		wg.Wait()
		for _, err := range errs {
			require.NoError(t, err)
		}
		assert.Equal(t, int32(1), peak.Load())
	})
}

// fakeGoBuild writes the binary that `go build -o` would produce.
func fakeGoBuild(cmd *exec.Cmd) error {
	if cmd.Args[0] != "go" {
		return nil
	}
	return os.WriteFile(cmd.Args[5], []byte("binary"), 0o755)
}

func TestRun(t *testing.T) {
	switch os.Getenv("ZINC_WEBBUILD_TEST_PHASE") {
	case "launch":
		require.NoError(t, os.Setenv("ZINC_WEBBUILD_TEST_PHASE", "child"))
		require.NoError(t, Run(os.Args[0], []string{"-test.run=^TestRun$", "--", "argument with spaces"}))
		return
	case "child":
		wd, err := os.Getwd()
		require.NoError(t, err)
		assert.Equal(t, "argument with spaces", os.Args[len(os.Args)-1])
		_, err = fmt.Fprintln(os.Stdout, "child cwd:", wd)
		require.NoError(t, err)
		return
	}
	binary, err := os.Executable()
	require.NoError(t, err)
	dir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	cmd := exec.Command(binary, "-test.run=^TestRun$")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "ZINC_WEBBUILD_TEST_PHASE=launch")
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "%s", output)
	assert.Contains(t, string(output), "child cwd: "+dir)
}

func TestRunMissingBinary(t *testing.T) {
	err := Run(filepath.Join(t.TempDir(), "missing"), nil)
	require.Error(t, err)
}
