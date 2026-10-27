package archtest

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path"
	"strconv"
	"strings"
)

// Scan every platform's source without executing or type-loading the server.
// Import resolution deliberately rejects dot imports of persistence packages.
// References (not only calls) count, so assigning a writer to a variable is visible.
func persistenceScan(tree fs.FS) (map[string]int, error) {
	found := map[string]int{}
	err := fs.WalkDir(tree, ".", func(file string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if file != "." && (entry.Name() == "testdata" || (file != "cmd" && file != "internal" && !strings.HasPrefix(file, "cmd/") && !strings.HasPrefix(file, "internal/"))) {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(file, ".go") || strings.HasSuffix(file, "_test.go") {
			return nil
		}
		data, err := fs.ReadFile(tree, file)
		if err != nil {
			return err
		}
		f, err := parser.ParseFile(token.NewFileSet(), file, data, 0)
		if err != nil {
			return err
		}
		imports := map[string]string{}
		for _, imp := range f.Imports {
			pkg, err := strconv.Unquote(imp.Path.Value)
			if err != nil {
				return err
			}
			name := path.Base(pkg)
			if imp.Name != nil {
				name = imp.Name.Name
			}
			if name == "." && persistencePackage(pkg) {
				return fmt.Errorf("%s: persistence imports must have an explicit name: %s", file, pkg)
			}
			imports[name] = pkg
		}
		for _, decl := range f.Decls {
			owner := "<package>"
			if fn, ok := decl.(*ast.FuncDecl); ok {
				owner = fn.Name.Name
				if fn.Recv != nil {
					owner = receiverName(fn.Recv.List[0].Type) + "." + owner
				}
			}
			ast.Inspect(decl, func(node ast.Node) bool {
				switch n := node.(type) {
				case *ast.SelectorExpr:
					ident, ok := n.X.(*ast.Ident)
					if !ok {
						break
					}
					pkg := imports[ident.Name]
					if persistenceSymbol(pkg, n.Sel.Name) {
						found[file+"|"+owner+"|"+pkg+"."+n.Sel.Name]++
					}
				case *ast.BasicLit:
					if n.Kind != token.STRING {
						break
					}
					value, err := strconv.Unquote(n.Value)
					if err != nil {
						break
					}
					if persistencePath(value) {
						found[file+"|path|"+value]++
					}
				}
				return true
			})
		}
		return nil
	})
	return found, err
}

func receiverName(expr ast.Expr) string {
	switch x := expr.(type) {
	case *ast.Ident:
		return x.Name
	case *ast.StarExpr:
		return "*" + receiverName(x.X)
	case *ast.IndexExpr:
		return receiverName(x.X)
	case *ast.IndexListExpr:
		return receiverName(x.X)
	default:
		return "<receiver>"
	}
}

func persistencePackage(pkg string) bool {
	return pkg == "os" || pkg == "syscall" || pkg == "golang.org/x/sys/unix" || pkg == "github.com/rainchan/webmux/internal/statefile"
}

func persistenceSymbol(pkg, name string) bool {
	switch pkg {
	case "os":
		switch name {
		case "WriteFile", "OpenFile", "Create", "CreateTemp", "Rename", "Link", "Remove", "RemoveAll", "Truncate", "NewFile":
			return true
		}
	case "syscall", "golang.org/x/sys/unix":
		switch name {
		case "Open", "Openat", "Creat", "Flock", "FcntlFlock", "Rename", "Renameat", "Renameat2", "Unlink", "Unlinkat", "Link", "Linkat", "Truncate":
			return true
		}
	case "github.com/rainchan/webmux/internal/statefile":
		return name == "Write" || name == "WriteJSON" || name == "Open" || name == "OpenRuntime" || name == "ReadSnapshot" || name == "ReadRuntime"
	}
	return false
}

func persistencePath(value string) bool {
	// Temporary patterns and external JSON filenames are included: exclusions
	// must be explicit, reviewed inventory entries rather than filename guesses.
	return (strings.HasSuffix(value, ".json") || strings.HasSuffix(value, ".lock")) && !strings.ContainsAny(value, "\n\r\t ")
}
