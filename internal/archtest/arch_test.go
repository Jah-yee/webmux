// Package archtest enforces the controller/service layering of
// docs/architecture.md. Its rules read type-checked syntax, so import
// aliases, dot imports and function values cannot hide a violation. The
// rules run on this module (TestArchitecture) and on the deliberately
// violating fixture under testdata/fixture (rules_test.go).
package archtest

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/tools/go/packages"
)

type layer string

const (
	controller layer = "controller"
	service    layer = "service"
	shared     layer = "shared"
	client     layer = "client"
	testOnly   layer = "test-only"
	// assemblyRoot is every package under cmd/.
	assemblyRoot layer = "assembly root"
)

// manifest classifies the packages of one module. Paths are relative to the
// module root.
type manifest struct {
	module string
	// layers holds every package under internal/.
	layers map[string]layer
	// outbound lists service packages that send HTTP requests, with the
	// reason. They may use net/http client symbols only.
	outbound map[string]string
	// pure lists stateless service package functions controllers may call,
	// with a reviewable reason. Constructors and stateful functions remain
	// forbidden; purity is a reviewed contract, not inferred from a name.
	pure map[string]string
	// contract is the wire type package.
	contract string
	// wireScope are the packages whose JSON values must be contract types.
	wireScope []string
	// sinks are the scope's own JSON wrappers: "pkg.func" to the index of
	// the value argument, or of the type parameter for a generic wrapper.
	sinks map[string]sink
	// encoders are the service functions ("pkg.func" or "pkg.(*T).method")
	// allowed to encode JSON, with the reason: outbound protocol payloads
	// and internal digests, never client-visible wire shapes.
	encoders map[string]string
}

// sink locates the JSON value of a call: argument arg, or type argument
// typeArg when typeArg >= 0.
type sink struct {
	arg, typeArg int
}

type violation struct {
	file string
	line int
	rule string
	msg  string
	hint string
}

func (v violation) String() string {
	if v.line == 0 {
		return fmt.Sprintf("%s [%s] %s — %s", v.file, v.rule, v.msg, v.hint)
	}
	return fmt.Sprintf("%s:%d [%s] %s — %s", v.file, v.line, v.rule, v.msg, v.hint)
}

type program struct {
	dir  string
	m    manifest
	fset *token.FileSet
	// pkgs are the module's packages keyed by relative path.
	pkgs map[string]*packages.Package
	out  []violation
}

// load type-checks every package of the module in dir once; all rules share
// the result. Test files are not loaded: the rules govern shipped code.
func load(dir string, m manifest) (*program, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	cfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedImports | packages.NeedDeps |
			packages.NeedSyntax | packages.NeedTypes | packages.NeedTypesInfo,
		Dir:   abs,
		Tests: false,
	}
	list, err := packages.Load(cfg, "./...")
	if err != nil {
		return nil, err
	}
	p := &program{dir: abs, m: m, pkgs: map[string]*packages.Package{}}
	var problems []string
	for _, pkg := range list {
		for _, e := range pkg.Errors {
			problems = append(problems, e.Error())
		}
		if rel, ok := p.rel(pkg.PkgPath); ok {
			p.pkgs[rel] = pkg
			p.fset = pkg.Fset
		}
	}
	if len(problems) > 0 {
		return nil, fmt.Errorf("load %s:\n%s", dir, strings.Join(problems, "\n"))
	}
	return p, nil
}

// rel returns path relative to the module, and whether it is in the module.
func (p *program) rel(path string) (string, bool) {
	if path == p.m.module {
		return "", true
	}
	rel, ok := strings.CutPrefix(path, p.m.module+"/")
	return rel, ok
}

func (p *program) layerOf(rel string) layer {
	if rel == "cmd" || strings.HasPrefix(rel, "cmd/") {
		return assemblyRoot
	}
	return p.m.layers[rel]
}

// pkgLayer is the layer of an import path; "" outside the module or for an
// unrestricted top-level package.
func (p *program) pkgLayer(path string) layer {
	if rel, ok := p.rel(path); ok {
		return p.layerOf(rel)
	}
	return ""
}

func (p *program) report(pos token.Pos, rule, hint, format string, args ...any) {
	position := p.fset.Position(pos)
	file := position.Filename
	if rel, err := filepath.Rel(p.dir, file); err == nil {
		file = filepath.ToSlash(rel)
	}
	p.out = append(p.out, violation{file, position.Line, rule, fmt.Sprintf(format, args...), hint})
}

// reportPackage records a package-level violation that has no source line.
func (p *program) reportPackage(rel, rule, hint, format string, args ...any) {
	p.out = append(p.out, violation{rel, 0, rule, fmt.Sprintf(format, args...), hint})
}

// sorted returns the violations ordered by file and line, without duplicates.
func (p *program) sorted() []violation {
	sort.Slice(p.out, func(i, j int) bool {
		a, b := p.out[i], p.out[j]
		if a.file != b.file {
			return a.file < b.file
		}
		if a.line != b.line {
			return a.line < b.line
		}
		if a.rule != b.rule {
			return a.rule < b.rule
		}
		return a.msg < b.msg
	})
	var out []violation
	for i, v := range p.out {
		if i == 0 || v != p.out[i-1] {
			out = append(out, v)
		}
	}
	return out
}

// check runs every rule and returns the sorted violations.
func (p *program) check() []violation {
	p.checkManifest()
	p.checkPureFunctions()
	for _, rel := range p.relPaths() {
		pkg := p.pkgs[rel]
		l := p.layerOf(rel)
		p.checkImports(rel, l, pkg)
		if p.inWireScope(rel) {
			p.checkWireValues(rel, pkg)
		}
		if rel == p.m.contract {
			p.checkContractTypes(pkg)
		}
		switch l {
		case controller:
			p.checkControllerUses(pkg)
		case service:
			p.checkServiceHTTP(rel, pkg)
			p.checkServiceErrors(pkg)
			p.checkServiceJSON(pkg)
		}
	}
	return p.sorted()
}

func (p *program) relPaths() []string {
	paths := make([]string, 0, len(p.pkgs))
	for rel := range p.pkgs {
		paths = append(paths, rel)
	}
	sort.Strings(paths)
	return paths
}

// R0: every package under internal/ is classified, and no entry is stale.
func (p *program) checkManifest() {
	for _, rel := range p.relPaths() {
		if strings.HasPrefix(rel, "internal/") && p.m.layers[rel] == "" {
			p.reportPackage(rel, "R0", "add it to the layer manifest in internal/archtest/layers_test.go", "package %s is not classified", rel)
		}
	}
	for rel := range p.m.layers {
		if _, ok := p.pkgs[rel]; !ok {
			p.reportPackage(rel, "R0", "remove the stale entry from the layer manifest", "manifest lists %s, which does not exist", rel)
		}
	}
	for key := range p.m.encoders {
		rel, _, _ := strings.Cut(key, ".")
		if p.m.layers[rel] != service {
			p.reportPackage(rel, "R0", "only service functions register JSON encoding", "encoder registration %s is not in a service package", key)
		}
	}
	for rel := range p.m.outbound {
		if p.m.layers[rel] != service {
			p.reportPackage(rel, "R0", "only service packages register outbound HTTP", "outbound registration %s is not a service package", rel)
		}
	}
}

func isWebSocket(path string) bool {
	return path == "github.com/coder/websocket" || strings.HasPrefix(path, "github.com/coder/websocket/")
}

// controllerForbiddenImport names the process, syscall, PTY and persistence
// packages a controller must reach through a service.
func (p *program) controllerForbiddenImport(path string) bool {
	switch {
	case path == "os/exec", path == "syscall":
		return true
	case path == "golang.org/x/sys" || strings.HasPrefix(path, "golang.org/x/sys/"):
		return true
	case path == "github.com/creack/pty" || strings.HasPrefix(path, "github.com/creack/pty/"):
		return true
	case path == p.m.module+"/internal/statefile":
		return true
	}
	return false
}

// R1 dependency direction, R2 the api package's importers and R3 the
// controller's forbidden imports.
func (p *program) checkImports(rel string, l layer, pkg *packages.Package) {
	api := p.m.module + "/internal/api"
	for _, file := range pkg.Syntax {
		for _, spec := range file.Imports {
			path := strings.Trim(spec.Path.Value, `"`)
			target := p.pkgLayer(path)
			if path == api && l != assemblyRoot && rel != "internal/api" {
				p.report(spec.Pos(), "R2", "only cmd/ wires the HTTP handler; depend on internal/contract or a shared package instead", "%s imports internal/api", rel)
				continue
			}
			switch l {
			case service:
				switch {
				case target == controller || target == assemblyRoot:
					p.report(spec.Pos(), "R1", "services must not depend on transport or wiring; move the shared part to a shared package", "service %s imports %s %s", rel, target, path)
				case isWebSocket(path), path == "net/http/httptest":
					p.report(spec.Pos(), "R1", "WebSocket and HTTP test servers belong to the controller", "service %s imports %s", rel, path)
				}
			case shared, client:
				if rel == p.m.contract && (target == controller || target == service) {
					p.report(spec.Pos(), "R10", "wire types may use the standard library only; convert in internal/wire", "contract imports %s %s", target, path)
				} else if target == controller || target == service {
					p.report(spec.Pos(), "R1", "move the needed code into a shared package (for example internal/pathutil or internal/contract)", "%s package %s imports %s %s", l, rel, target, path)
				}
			case controller:
				if p.controllerForbiddenImport(path) {
					p.report(spec.Pos(), "R3", "move process, PTY and persistence work into a service and call its method", "controller %s imports %s", rel, path)
				}
			}
		}
	}
}

// osFilesystem are the os functions that read, create, change or remove
// filesystem entries.
var osFilesystem = map[string]bool{
	"ReadDir": true, "Stat": true, "Lstat": true, "Open": true, "OpenFile": true, "OpenInRoot": true,
	"OpenRoot": true, "Create": true, "CreateTemp": true, "Mkdir": true, "MkdirAll": true, "MkdirTemp": true,
	"Remove": true, "RemoveAll": true, "Rename": true, "Chmod": true, "Chown": true, "Lchown": true,
	"Chtimes": true, "ReadFile": true, "WriteFile": true, "Symlink": true, "Link": true, "Readlink": true,
	"Truncate": true, "CopyFS": true, "DirFS": true,
}

// Pure registrations must name real package functions in services, explain
// their stateless contract, and still be used by a controller.
func (p *program) checkPureFunctions() {
	used := map[string]bool{}
	for rel, pkg := range p.pkgs {
		if p.layerOf(rel) != controller {
			continue
		}
		for _, obj := range pkg.TypesInfo.Uses {
			if fn, ok := obj.(*types.Func); ok && fn.Pkg() != nil {
				used[p.sinkKey(fn.Origin())] = true
			}
		}
	}
	for key, reason := range p.m.pure {
		rel, name, _ := strings.Cut(key, ".")
		if strings.TrimSpace(reason) == "" {
			p.reportPackage(rel, "R0", "document why this function is pure", "pure registration %s has no reason", key)
		}
		if p.m.layers[rel] != service {
			p.reportPackage(rel, "R0", "only service functions need pure registrations", "pure registration %s is not in a service package", key)
			continue
		}
		pkg := p.pkgs[rel]
		var fn *types.Func
		if pkg != nil {
			fn, _ = pkg.Types.Scope().Lookup(name).(*types.Func)
		}
		if fn == nil {
			p.reportPackage(rel, "R0", "remove the stale entry from the pure registrations", "registered pure function %s is not a package function", key)
			continue
		}
		if !used[key] {
			p.reportPackage(rel, "R0", "remove the stale entry from the pure registrations", "registered pure function %s is not used by a controller", key)
		}
	}
}

// R4 filesystem access and R5 service package-level functions in a
// controller. Every use of the function object counts, so calls through an
// alias, a dot import or a function value are all reported.
func (p *program) checkControllerUses(pkg *packages.Package) {
	for id, obj := range pkg.TypesInfo.Uses {
		fn, ok := obj.(*types.Func)
		if !ok || fn.Pkg() == nil {
			continue
		}
		fn = fn.Origin()
		sig, _ := fn.Type().(*types.Signature)
		if sig == nil || sig.Recv() != nil {
			continue
		}
		path := fn.Pkg().Path()
		switch {
		case path == "os" && osFilesystem[fn.Name()]:
			p.report(id.Pos(), "R4", "move the filesystem work into a service method (for example internal/files)", "controller uses os.%s", fn.Name())
		case p.pkgLayer(path) == service && strings.TrimSpace(p.m.pure[p.sinkKey(fn)]) == "":
			p.report(id.Pos(), "R5", "call a method of the injected service; construct services in cmd/", "controller uses service function %s.%s", fn.Pkg().Name(), fn.Name())
		}
	}
}

// httpServer are net/http symbols that only an HTTP server uses.
var httpServer = map[string]bool{
	"ResponseWriter": true, "Handler": true, "HandlerFunc": true, "ServeMux": true, "NewServeMux": true,
	"Server": true, "Error": true, "Redirect": true, "MaxBytesReader": true, "MaxBytesHandler": true,
	"NotFound": true, "NotFoundHandler": true, "ListenAndServe": true, "ListenAndServeTLS": true,
	"Serve": true, "ServeTLS": true, "ServeContent": true, "ServeFile": true, "FileServer": true,
	"Handle": true, "HandleFunc": true, "ResponseController": true, "NewResponseController": true,
	"Flusher": true, "Hijacker": true, "StripPrefix": true, "TimeoutHandler": true, "SetCookie": true,
}

// httpClient are the net/http symbols a registered outbound service may use.
func httpClient(name string) bool {
	switch name {
	case "Client", "NewRequest", "NewRequestWithContext", "Request", "Response", "Header",
		"RoundTripper", "Transport", "DefaultTransport", "DefaultClient", "NoBody", "StatusText",
		"ErrUseLastResponse", "ProxyFromEnvironment":
		return true
	}
	return strings.HasPrefix(name, "Status") || strings.HasPrefix(name, "Method")
}

func isHTTPRequestPointer(t types.Type) bool {
	ptr, ok := t.(*types.Pointer)
	if !ok {
		return false
	}
	named, ok := ptr.Elem().(*types.Named)
	return ok && named.Obj().Pkg() != nil && named.Obj().Pkg().Path() == "net/http" && named.Obj().Name() == "Request"
}

// R6: services are transport-agnostic. Server symbols and *http.Request
// parameters are always reported; other net/http symbols only outside the
// registered outbound packages, and there only beyond the client symbols.
func (p *program) checkServiceHTTP(rel string, pkg *packages.Package) {
	_, outbound := p.m.outbound[rel]
	for id, obj := range pkg.TypesInfo.Uses {
		if obj.Pkg() == nil || obj.Pkg().Path() != "net/http" || obj.Parent() != obj.Pkg().Scope() {
			continue
		}
		name := obj.Name()
		switch {
		case httpServer[name]:
			p.report(id.Pos(), "R6", "keep HTTP serving in the controller; return data and error kinds instead", "service uses server symbol http.%s", name)
		case !outbound:
			p.report(id.Pos(), "R6", "register the package as outbound in layers_test.go if it sends HTTP requests", "unregistered service uses http.%s", name)
		case !httpClient(name):
			p.report(id.Pos(), "R6", "outbound services may use net/http client symbols only", "outbound service uses http.%s", name)
		}
	}
	// Declared functions only: an outbound client hook such as
	// http.Client.CheckRedirect is a literal that must take *http.Request.
	for _, file := range pkg.Syntax {
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok {
				continue
			}
			for _, field := range fn.Type.Params.List {
				if isHTTPRequestPointer(pkg.TypesInfo.TypeOf(field.Type)) {
					p.report(field.Pos(), "R6", "parse the request in the controller and pass plain values", "service function %s takes *http.Request", fn.Name.Name)
				}
			}
		}
	}
}

var errorInterface = types.Universe.Lookup("error").Type().Underlying().(*types.Interface)

func isInteger(t types.Type) bool {
	basic, ok := t.Underlying().(*types.Basic)
	return ok && basic.Info()&types.IsInteger != 0
}

func isError(t types.Type) bool {
	return types.Implements(t, errorInterface) || types.Implements(types.NewPointer(t), errorInterface)
}

func isHTTPStatusName(name string) bool {
	switch strings.ToLower(name) {
	case "status", "statuscode", "httpstatus", "httpstatuscode":
		return true
	}
	return false
}

// R7: errors carry domain data, not HTTP statuses. Reject explicitly named
// integer status fields and status-returning error methods/mappers. Explicit
// HTTPStatus/HTTPStatusCode helpers are transport concerns regardless of their
// inputs; domain integers and non-error Status methods remain allowed.
func (p *program) checkServiceErrors(pkg *packages.Package) {
	for _, file := range pkg.Syntax {
		for _, decl := range file.Decls {
			switch decl := decl.(type) {
			case *ast.GenDecl:
				for _, spec := range decl.Specs {
					ts, ok := spec.(*ast.TypeSpec)
					if !ok {
						continue
					}
					obj := pkg.TypesInfo.Defs[ts.Name]
					if obj == nil {
						continue
					}
					t := obj.Type()
					if !isError(t) {
						continue
					}
					st, ok := t.Underlying().(*types.Struct)
					if !ok {
						continue
					}
					for i := range st.NumFields() {
						if field := st.Field(i); isHTTPStatusName(field.Name()) && isInteger(field.Type()) {
							p.report(field.Pos(), "R7", "wrap a sentinel error kind and map it to a status in internal/api/errors.go", "error type %s has HTTP status field %s", ts.Name.Name, field.Name())
						}
					}
				}
			case *ast.FuncDecl:
				if !isHTTPStatusName(decl.Name.Name) {
					continue
				}
				sig, _ := pkg.TypesInfo.Defs[decl.Name].Type().(*types.Signature)
				if sig == nil {
					continue
				}
				errorStatus := strings.HasPrefix(strings.ToLower(decl.Name.Name), "http")
				if recv := sig.Recv(); recv != nil {
					errorStatus = errorStatus || isError(recv.Type())
				}
				for i := range sig.Params().Len() {
					errorStatus = errorStatus || isError(sig.Params().At(i).Type())
				}
				if !errorStatus {
					continue
				}
				for i := range sig.Results().Len() {
					if isInteger(sig.Results().At(i).Type()) {
						p.report(decl.Name.Pos(), "R7", "return an error kind; the controller owns HTTP statuses", "service declares error status mapper %s returning an integer", decl.Name.Name)
						break
					}
				}
			}
		}
	}
}

func (p *program) inWireScope(rel string) bool {
	for _, scope := range p.m.wireScope {
		if rel == scope {
			return true
		}
	}
	return false
}

// jsonSinks are the encoding/json entry points and their value argument.
var jsonSinks = map[string]int{
	"Marshal": 0, "MarshalIndent": 0, "Unmarshal": 1, "(*Encoder).Encode": 0, "(*Decoder).Decode": 0,
}

// sinkKey names fn as "pkg.Func" or "pkg.(*T).Method", with pkg relative to
// the module for module code.
func (p *program) sinkKey(fn *types.Func) string {
	path := fn.Pkg().Path()
	if rel, ok := p.rel(path); ok {
		path = rel
	}
	name := fn.Name()
	if recv := fn.Type().(*types.Signature).Recv(); recv != nil {
		t := recv.Type()
		star := ""
		if ptr, ok := t.(*types.Pointer); ok {
			t, star = ptr.Elem(), "*"
		}
		if named, ok := t.(*types.Named); ok {
			name = "(" + star + named.Obj().Name() + ")." + name
		}
	}
	return path + "." + name
}

func (p *program) sinkOf(fn *types.Func) (sink, bool) {
	if fn.Pkg() == nil {
		return sink{}, false
	}
	key := p.sinkKey(fn)
	if name, ok := strings.CutPrefix(key, "encoding/json."); ok {
		if arg, ok := jsonSinks[name]; ok {
			return sink{arg: arg, typeArg: -1}, true
		}
	}
	s, ok := p.m.sinks[key]
	return s, ok
}

// wireType reports whether t, after pointers, slices, arrays and map
// elements, is a named contract type. Interfaces and type parameters are
// not: they defer the check to whoever supplies the value.
func (p *program) wireType(t types.Type) (ok, deferred bool) {
	for {
		t = types.Unalias(t)
		switch u := t.(type) {
		case *types.Pointer:
			t = u.Elem()
			continue
		case *types.Slice:
			t = u.Elem()
			continue
		case *types.Array:
			t = u.Elem()
			continue
		case *types.Map:
			t = u.Elem()
			continue
		case *types.TypeParam:
			return false, true
		case *types.Named:
			if _, isInterface := u.Underlying().(*types.Interface); isInterface {
				return false, true
			}
			obj := u.Obj()
			return obj.Pkg() != nil && obj.Pkg().Path() == p.m.module+"/"+p.m.contract, false
		case *types.Interface:
			return false, true
		}
		return false, false
	}
}

// R8: every JSON value the controller encodes or decodes is a contract
// type. A value of interface or type-parameter type passes only inside a
// registered wrapper, whose own call sites are checked instead. A sink used
// as a function value would escape the check and is reported too.
func (p *program) checkWireValues(rel string, pkg *packages.Package) {
	info := pkg.TypesInfo
	called := map[*ast.Ident]bool{}
	for _, file := range pkg.Syntax {
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			wrapper := false
			if obj, ok := info.Defs[fn.Name].(*types.Func); ok {
				_, wrapper = p.m.sinks[p.sinkKey(obj)]
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				id := calleeIdent(call.Fun)
				if id == nil {
					return true
				}
				called[id] = true
				callee, ok := info.Uses[id].(*types.Func)
				if !ok {
					return true
				}
				s, ok := p.sinkOf(callee.Origin())
				if !ok {
					return true
				}
				var t types.Type
				what := "value"
				if s.typeArg >= 0 {
					inst, ok := info.Instances[id]
					if !ok || inst.TypeArgs.Len() <= s.typeArg {
						return true
					}
					t, what = inst.TypeArgs.At(s.typeArg), "type argument"
				} else if s.arg < len(call.Args) {
					t = info.TypeOf(call.Args[s.arg])
				} else {
					return true
				}
				good, deferred := p.wireType(t)
				switch {
				case good, deferred && wrapper:
				case deferred:
					p.report(call.Pos(), "R8", "pass a concrete contract type, or register the wrapper in layers_test.go", "%s %s is %s outside a registered JSON wrapper", p.sinkKey(callee.Origin()), what, t)
				default:
					p.report(call.Pos(), "R8", "declare the shape in internal/contract and convert in internal/wire", "%s %s has non-contract type %s", p.sinkKey(callee.Origin()), what, t)
				}
				return true
			})
		}
	}
	for id, obj := range info.Uses {
		fn, ok := obj.(*types.Func)
		if !ok || fn.Pkg() == nil || called[id] {
			continue
		}
		if _, ok := p.sinkOf(fn.Origin()); ok {
			p.report(id.Pos(), "R8", "call JSON sinks directly so their values can be checked", "%s used as a function value", p.sinkKey(fn.Origin()))
		}
	}
}

// calleeIdent is the identifier naming a call's function, through package
// selectors, method selectors and explicit instantiation.
func calleeIdent(fun ast.Expr) *ast.Ident {
	switch f := ast.Unparen(fun).(type) {
	case *ast.Ident:
		return f
	case *ast.SelectorExpr:
		return f.Sel
	case *ast.IndexExpr:
		return calleeIdent(f.X)
	case *ast.IndexListExpr:
		return calleeIdent(f.X)
	}
	return nil
}

// R8: contract types never embed service or controller types, at any depth.
func (p *program) checkContractTypes(pkg *packages.Package) {
	scope := pkg.Types.Scope()
	for _, name := range scope.Names() {
		obj, ok := scope.Lookup(name).(*types.TypeName)
		if !ok {
			continue
		}
		seen := map[types.Type]bool{}
		var walk func(types.Type) (string, bool)
		walk = func(t types.Type) (string, bool) {
			t = types.Unalias(t)
			if seen[t] {
				return "", false
			}
			seen[t] = true
			switch u := t.(type) {
			case *types.Named:
				if o := u.Obj(); o.Pkg() != nil {
					if l := p.pkgLayer(o.Pkg().Path()); l == service || l == controller {
						return o.Pkg().Name() + "." + o.Name(), true
					}
				}
				return walk(u.Underlying())
			case *types.Pointer:
				return walk(u.Elem())
			case *types.Slice:
				return walk(u.Elem())
			case *types.Array:
				return walk(u.Elem())
			case *types.Map:
				if s, bad := walk(u.Key()); bad {
					return s, bad
				}
				return walk(u.Elem())
			case *types.Struct:
				for i := range u.NumFields() {
					if s, bad := walk(u.Field(i).Type()); bad {
						return s, bad
					}
				}
			}
			return "", false
		}
		if inner, bad := walk(obj.Type()); bad {
			p.report(obj.Pos(), "R8", "copy the fields into contract types; convert in internal/wire", "contract type %s contains %s", obj.Name(), inner)
		}
	}
}

// jsonEncoders are the encoding/json functions that produce JSON.
var jsonEncoders = map[string]bool{"Marshal": true, "MarshalIndent": true, "(*Encoder).Encode": true}

// R9: services do not encode client-visible JSON. They declare no
// MarshalJSON and call encoders directly only in registered function bodies.
// Every source reference is checked, including package initializers and
// aliases, which could otherwise escape the registered function.
// Persistence goes through internal/statefile, outside this rule.
func (p *program) checkServiceJSON(pkg *packages.Package) {
	info := pkg.TypesInfo
	used := map[string]bool{}
	called := map[*ast.Ident]bool{}
	owners := map[*ast.Ident]string{}
	for _, file := range pkg.Syntax {
		ast.Inspect(file, func(n ast.Node) bool {
			if call, ok := n.(*ast.CallExpr); ok {
				called[calleeIdent(call.Fun)] = true
			}
			return true
		})
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok {
				continue
			}
			if fn.Name.Name == "MarshalJSON" && fn.Recv != nil {
				p.report(fn.Name.Pos(), "R9", "define the wire shape, and its MarshalJSON, in internal/contract", "service declares MarshalJSON")
			}
			if fn.Body == nil {
				continue
			}
			key := p.sinkKey(info.Defs[fn.Name].(*types.Func))
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				if id, ok := n.(*ast.Ident); ok {
					owners[id] = key
				}
				return true
			})
		}
	}
	for id, obj := range info.Uses {
		callee, ok := obj.(*types.Func)
		if !ok || callee.Pkg() == nil || callee.Pkg().Path() != "encoding/json" {
			continue
		}
		name := strings.TrimPrefix(p.sinkKey(callee.Origin()), "encoding/json.")
		if !jsonEncoders[name] {
			continue
		}
		key := owners[id]
		_, registered := p.m.encoders[key]
		switch {
		case !called[id]:
			p.report(id.Pos(), "R9", "call JSON encoders directly inside registered functions", "json.%s used as a function value", name)
		case registered:
			used[key] = true
		case key == "":
			p.report(id.Pos(), "R9", "encode JSON only inside registered functions", "package initializer calls json.%s", name)
		default:
			p.report(id.Pos(), "R9", "return data and let the controller encode a contract type, or register the function in layers_test.go", "service function %s calls json.%s", key, name)
		}
	}
	for key := range p.m.encoders {
		if rel, ok := p.rel(pkg.PkgPath); ok && strings.HasPrefix(key, rel+".") && !used[key] {
			p.reportPackage(rel, "R0", "remove the stale entry from the encoder registrations", "registered encoder %s does not encode JSON", key)
		}
	}
}
