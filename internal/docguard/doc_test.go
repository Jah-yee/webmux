// Package docguard enforces the documentation size and tree-layout rules in
// docs/doc-governance.md. It is test-only: TestDocsGovernance runs the rules on
// this repository, and TestRules exercises them on an in-memory tree.
package docguard

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"testing/fstest"
)

const (
	maxLines     = 400
	minLines     = 20
	maxBytes     = 48 << 10
	maxChildDirs = 20
)

var roots = []string{"docs", "openspec"}

type violation struct {
	path string
	rule string
	msg  string
}

// TestDocsGovernance is the failing gate. Its output is addressed to the next
// agent: it names the file, its measured size and the required reorganization.
func TestDocsGovernance(t *testing.T) {
	fsys := os.DirFS(filepath.Join("..", ".."))
	violations, err := check(fsys)
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) == 0 {
		return
	}
	t.Error(report(violations))
}

func check(fsys fs.FS) ([]violation, error) {
	var out []violation
	for _, root := range roots {
		err := fs.WalkDir(fsys, root, func(name string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if !skipFanout(name) {
					n, err := childDirs(fsys, name)
					if err != nil {
						return err
					}
					if n > maxChildDirs {
						out = append(out, violation{name, "fanout", fanoutMessage(name, n)})
					}
				}
				return nil
			}
			if !strings.EqualFold(path.Ext(name), ".md") || skipSize(name) {
				return nil
			}
			data, err := fs.ReadFile(fsys, name)
			if err != nil {
				return err
			}
			out = append(out, checkFile(fsys, name, data)...)
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].path != out[j].path {
			return out[i].path < out[j].path
		}
		return out[i].rule < out[j].rule
	})
	return out, nil
}

func checkFile(fsys fs.FS, name string, data []byte) []violation {
	var out []violation
	n := lineCount(data)
	if n > maxLines {
		out = append(out, violation{name, "lines", maxLinesMessage(name, n)})
	}
	if len(data) > maxBytes {
		out = append(out, violation{name, "bytes", maxBytesMessage(name, len(data))})
	}
	if !skipMin(fsys, name) && n < minLines && len(data) <= maxBytes {
		out = append(out, violation{name, "short", minLinesMessage(name, n)})
	}
	return out
}

func lineCount(data []byte) int {
	if len(data) == 0 {
		return 0
	}
	n := bytes.Count(data, []byte("\n"))
	if !bytes.HasSuffix(data, []byte("\n")) {
		n++
	}
	return n
}

func childDirs(fsys fs.FS, dir string) (int, error) {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, entry := range entries {
		if entry.IsDir() {
			n++
		}
	}
	return n, nil
}

// Archive records are immutable history.
func skipSize(name string) bool {
	return strings.HasPrefix(name, "openspec/changes/archive/")
}

// OpenSpec change artifacts are naturally short, as are index files.
func skipMin(fsys fs.FS, name string) bool {
	if strings.HasPrefix(name, "openspec/changes/") || strings.EqualFold(path.Base(name), "README.md") {
		return true
	}
	info, err := fs.Stat(fsys, strings.TrimSuffix(name, path.Ext(name)))
	return err == nil && info.IsDir()
}

// Tool-mandated flat trees keep their layout; everything else follows the
// governance rules.
func skipFanout(dir string) bool {
	switch {
	case dir == "docs/images" || strings.HasPrefix(dir, "docs/images/"):
		return true
	case dir == "openspec/specs":
		return true
	case dir == "openspec/changes/archive" || strings.HasPrefix(dir, "openspec/changes/archive/"):
		return true
	}
	return false
}

func maxLinesMessage(name string, n int) string {
	if strings.HasPrefix(name, "openspec/") && strings.HasSuffix(name, "/spec.md") {
		return fmt.Sprintf("%s：%d 行，超过上限 %d 行。OpenSpec 要求每个 capability 只保留一个 spec.md；请按语义把这个 capability 拆成多个新的 capability 目录（每个仍是 openspec/specs/<新capability>/spec.md），在变更中迁移需求并更新引用，不要在同目录拆分 spec.md。", name, n, maxLines)
	}
	dir := strings.TrimSuffix(name, ".md") + "/"
	return fmt.Sprintf("%s：%d 行，超过上限 %d 行。请按主题拆分为 %s<主题>.md，保留 %s 作为树状索引并更新所有引用；每个子文件保持 %d–%d 行且不超过 %d KiB。", name, n, maxLines, dir, name, minLines, maxLines, maxBytes>>10)
}

func maxBytesMessage(name string, n int) string {
	dir := strings.TrimSuffix(name, ".md") + "/"
	return fmt.Sprintf("%s：%d 字节（%.1f KiB），超过上限 %d 字节（%d KiB）。行数不是唯一成本，宽表格也会拖慢阅读；请按主题拆分为 %s<主题>.md，保留 %s 作为树状索引并更新所有引用；每个子文件保持 %d–%d 行且不超过 %d KiB。", name, n, float64(n)/(1<<10), maxBytes, maxBytes>>10, dir, name, minLines, maxLines, maxBytes>>10)
}

func minLinesMessage(name string, n int) string {
	return fmt.Sprintf("%s：%d 行，低于下限 %d 行。请把它合并到语义最近的同主题文档，更新所有引用后删除本文件；不要保留过短的孤立 Markdown 文件。", name, n, minLines)
}

func fanoutMessage(dir string, n int) string {
	return fmt.Sprintf("%s：直接子目录 %d 个，超过上限 %d 个。请按语义把这些子目录归并为更少的上级分组（例如 %s/<主题>/<子主题>/），更新所有链接；每一级直接子目录不超过 %d 个。", dir, n, maxChildDirs, dir, maxChildDirs)
}

func report(violations []violation) string {
	var b strings.Builder
	fmt.Fprintf(&b, "文档治理校验未通过（%d 项）。请阅读 docs/doc-governance.md，并按以下指令整理；拆分时保留旧路径作为索引并更新引用，合并时选择语义最近的文档。", len(violations))
	for _, v := range violations {
		fmt.Fprintf(&b, "\n\n%s", v.msg)
	}
	return b.String()
}

func TestRules(t *testing.T) {
	lines := func(n int) string {
		return strings.Repeat("- 一行\n", n)
	}
	fsys := fstest.MapFS{}
	add := func(name, data string) {
		fsys[name] = &fstest.MapFile{Data: []byte(data)}
	}

	add("docs/ok.md", lines(minLines))
	exact := lines(maxLines-1) + "- 一行"
	add("docs/exact.md", exact+strings.Repeat("x", maxBytes-len(exact)))
	add("docs/long.md", lines(maxLines+1))
	add("docs/heavy.md", lines(minLines)+strings.Repeat("x", maxBytes))
	add("docs/short.md", lines(minLines-1))
	add("docs/wide-short.md", lines(5)+strings.Repeat("x", maxBytes))
	add("docs/index/README.md", "# 索引\n")
	add("docs/tree.md", "# 索引\n")
	add("docs/tree/note.md", lines(minLines))
	add("openspec/specs/big/spec.md", lines(maxLines+1))
	add("openspec/changes/work/specs/cap/spec.md", lines(5))
	add("openspec/changes/work/specs/big/spec.md", lines(maxLines+1))
	add("openspec/changes/archive/2026-01-01-old/design.md", lines(maxLines+1)+strings.Repeat("x", maxBytes))
	for i := range 21 {
		add(fmt.Sprintf("docs/wide/d%02d/note.md", i), lines(minLines))
		add(fmt.Sprintf("openspec/specs/cap%02d/spec.md", i), lines(minLines))
		add(fmt.Sprintf("docs/images/group%02d/note.md", i), lines(minLines))
		add(fmt.Sprintf("openspec/changes/archive/2026-01-01-d%02d/design.md", i), lines(minLines))
		add(fmt.Sprintf("openspec/changes/wide/c%02d/design.md", i), lines(minLines))
	}

	violations, err := check(fsys)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"docs/heavy.md|bytes",
		"docs/long.md|lines",
		"docs/short.md|short",
		"docs/wide|fanout",
		"docs/wide-short.md|bytes",
		"openspec/changes/wide|fanout",
		"openspec/changes/work/specs/big/spec.md|lines",
		"openspec/specs/big/spec.md|lines",
	}
	var got []string
	for _, v := range violations {
		got = append(got, v.path+"|"+v.rule)
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("violations:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}

	messages := map[string]string{}
	for _, v := range violations {
		messages[v.path+"|"+v.rule] = v.msg
	}
	for key, texts := range map[string][]string{
		"docs/long.md|lines":                            {"超过上限 400 行", "docs/long/", "树状索引"},
		"docs/heavy.md|bytes":                           {"超过上限 49152 字节", "docs/heavy/"},
		"docs/short.md|short":                           {"低于下限 20 行", "合并"},
		"docs/wide-short.md|bytes":                      {"超过上限 49152 字节", "docs/wide-short/"},
		"docs/wide|fanout":                              {"直接子目录 21 个", "不超过 20 个"},
		"openspec/changes/work/specs/big/spec.md|lines": {"新的 capability", "不要在同目录拆分 spec.md"},
		"openspec/specs/big/spec.md|lines":              {"新的 capability", "不要在同目录拆分 spec.md"},
	} {
		for _, text := range texts {
			if !strings.Contains(messages[key], text) {
				t.Errorf("%s message %q missing %q", key, messages[key], text)
			}
		}
	}
	if !strings.Contains(report(violations), "docs/doc-governance.md") {
		t.Error("report does not point at docs/doc-governance.md")
	}
}
