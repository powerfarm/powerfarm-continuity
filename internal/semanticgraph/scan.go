package semanticgraph

import (
	"bufio"
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

type parsedFile struct {
	path       string
	rel        string
	pkgDir     string
	importPath string
	file       *ast.File
	fset       *token.FileSet
	imports    map[string]string
}

func Build(root string) (Bundle, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return Bundle{}, err
	}
	module, err := modulePath(abs)
	if err != nil {
		return Bundle{}, err
	}
	b := NewBuilder(module, abs)
	repoID := "repo:" + module
	b.AddNode(Node{ID: repoID, Kind: "repository", Name: filepath.Base(module), Qualified: module, Layer: "repository", Source: "go.mod"})

	files, err := parseGoFiles(abs, module)
	if err != nil {
		return Bundle{}, err
	}
	decls := collectDeclarations(b, repoID, files)
	collectRelations(b, files, decls)
	if err := addDomainProjection(b, abs); err != nil {
		return Bundle{}, err
	}
	addEffectSemantics(b)
	if err := addEngineProjection(b, abs); err != nil {
		return Bundle{}, err
	}
	return b.Bundle(), nil
}

func modulePath(root string) (string, error) {
	raw, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return "", err
	}
	re := regexp.MustCompile(`(?m)^module\s+([^\s]+)`)
	m := re.FindSubmatch(raw)
	if len(m) != 2 {
		return "", fmt.Errorf("go.mod has no module directive")
	}
	return string(m[1]), nil
}

func parseGoFiles(root, module string) ([]parsedFile, error) {
	var out []parsedFile
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			rel, _ := filepath.Rel(root, path)
			slash := filepath.ToSlash(rel)
			if slash == ".git" || strings.HasPrefix(slash, ".git/") || slash == "engines/downloaded" || strings.HasPrefix(slash, "engines/downloaded/") || slash == "engines/runtime" || strings.HasPrefix(slash, "engines/runtime/") || slash == "var" || strings.HasPrefix(slash, "var/") {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".go" {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		dir := filepath.Dir(rel)
		importPath := module
		if dir != "." {
			importPath += "/" + filepath.ToSlash(dir)
		}
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if err != nil {
			return fmt.Errorf("parse %s: %w", rel, err)
		}
		imports := map[string]string{}
		for _, im := range f.Imports {
			p := strings.Trim(im.Path.Value, `"`)
			alias := filepath.Base(p)
			if im.Name != nil {
				alias = im.Name.Name
			}
			imports[alias] = p
		}
		out = append(out, parsedFile{path: path, rel: filepath.ToSlash(rel), pkgDir: filepath.ToSlash(dir), importPath: importPath, file: f, fset: fset, imports: imports})
		return nil
	})
	sort.Slice(out, func(i, j int) bool { return out[i].rel < out[j].rel })
	return out, err
}

type declarations struct {
	funcs map[string]string
	types map[string]string
}

func collectDeclarations(b *Builder, repoID string, files []parsedFile) map[string]declarations {
	byPkg := map[string]declarations{}
	seenPkg := map[string]bool{}
	for _, pf := range files {
		layer := layerForPath(pf.rel)
		pkgID := "pkg:" + pf.importPath
		if !seenPkg[pkgID] {
			b.AddNode(Node{ID: pkgID, Kind: "package", Name: pf.file.Name.Name, Qualified: pf.importPath, Layer: layer, Source: pf.pkgDir})
			b.AddEdge(Edge{Kind: "contains", From: repoID, To: pkgID, Layer: "repository", Source: pf.rel, Confidence: "exact"})
			seenPkg[pkgID] = true
		}
		fileID := "file:" + pf.rel
		b.AddNode(Node{ID: fileID, Kind: "file", Name: filepath.Base(pf.rel), Qualified: pf.rel, Layer: layer, Source: pf.rel, Attributes: map[string]any{"test": strings.HasSuffix(pf.rel, "_test.go")}})
		b.AddEdge(Edge{Kind: "contains", From: pkgID, To: fileID, Layer: "repository", Source: pf.rel, Confidence: "exact"})
		d := byPkg[pf.importPath]
		if d.funcs == nil {
			d.funcs = map[string]string{}
			d.types = map[string]string{}
		}
		for _, decl := range pf.file.Decls {
			switch x := decl.(type) {
			case *ast.GenDecl:
				if x.Tok != token.TYPE && x.Tok != token.CONST && x.Tok != token.VAR {
					continue
				}
				for _, spec := range x.Specs {
					switch s := spec.(type) {
					case *ast.TypeSpec:
						id := "type:" + pf.importPath + "." + s.Name.Name
						pos := pf.fset.Position(s.Pos())
						end := pf.fset.Position(s.End())
						n := Node{ID: id, Kind: "type", Name: s.Name.Name, Qualified: pf.importPath + "." + s.Name.Name, Layer: layer, Source: pf.rel, Span: Span{StartLine: pos.Line, EndLine: end.Line}}
						if x.Doc != nil {
							n.Summary = firstSentence(x.Doc.Text())
						}
						b.AddNode(n)
						b.AddEdge(Edge{Kind: "defines", From: fileID, To: id, Layer: layer, Source: pf.rel, Line: pos.Line, Confidence: "exact"})
						d.types[s.Name.Name] = id
					case *ast.ValueSpec:
						kind := strings.ToLower(x.Tok.String())
						for _, name := range s.Names {
							id := kind + ":" + pf.importPath + "." + name.Name
							pos := pf.fset.Position(name.Pos())
							b.AddNode(Node{ID: id, Kind: kind, Name: name.Name, Qualified: pf.importPath + "." + name.Name, Layer: layer, Source: pf.rel, Span: Span{StartLine: pos.Line, EndLine: pos.Line}})
							b.AddEdge(Edge{Kind: "defines", From: fileID, To: id, Layer: layer, Source: pf.rel, Line: pos.Line, Confidence: "exact"})
						}
					}
				}
			case *ast.FuncDecl:
				recv := receiverName(x.Recv)
				kind := "function"
				id := "func:" + pf.importPath + "." + x.Name.Name
				qualified := pf.importPath + "." + x.Name.Name
				if recv != "" {
					kind = "method"
					id = "method:" + pf.importPath + "." + recv + "." + x.Name.Name
					qualified = pf.importPath + "." + recv + "." + x.Name.Name
				}
				pos := pf.fset.Position(x.Pos())
				end := pf.fset.Position(x.End())
				summary := ""
				if x.Doc != nil {
					summary = firstSentence(x.Doc.Text())
				}
				b.AddNode(Node{ID: id, Kind: kind, Name: x.Name.Name, Qualified: qualified, Layer: layer, Source: pf.rel, Span: Span{StartLine: pos.Line, EndLine: end.Line}, Summary: summary, Attributes: map[string]any{"receiver": recv, "test": strings.HasPrefix(x.Name.Name, "Test") && strings.HasSuffix(pf.rel, "_test.go")}})
				b.AddEdge(Edge{Kind: "defines", From: fileID, To: id, Layer: layer, Source: pf.rel, Line: pos.Line, Confidence: "exact"})
				if recv == "" {
					d.funcs[x.Name.Name] = id
				}
			}
		}
		byPkg[pf.importPath] = d
	}
	return byPkg
}

func collectRelations(b *Builder, files []parsedFile, decls map[string]declarations) {
	for _, pf := range files {
		pkgID := "pkg:" + pf.importPath
		for alias, imp := range pf.imports {
			if alias == "_" || alias == "." {
				continue
			}
			target := "pkg:" + imp
			kind := "package"
			if !strings.HasPrefix(imp, b.module) {
				kind = "external_package"
			}
			b.AddNode(Node{ID: target, Kind: kind, Name: filepath.Base(imp), Qualified: imp, Layer: func() string {
				if kind == "external_package" {
					return "dependency"
				}
				return layerForImport(imp)
			}()})
			b.AddEdge(Edge{Kind: "imports", From: pkgID, To: target, Layer: "code", Source: pf.rel, Confidence: "exact"})
		}
		for _, decl := range pf.file.Decls {
			switch x := decl.(type) {
			case *ast.GenDecl:
				if x.Tok != token.TYPE {
					continue
				}
				for _, spec := range x.Specs {
					ts, ok := spec.(*ast.TypeSpec)
					if !ok {
						continue
					}
					from := "type:" + pf.importPath + "." + ts.Name.Name
					ast.Inspect(ts.Type, func(n ast.Node) bool {
						switch t := n.(type) {
						case *ast.Ident:
							if target, ok := decls[pf.importPath].types[t.Name]; ok && target != from {
								b.AddEdge(Edge{Kind: "references_type", From: from, To: target, Layer: "code", Source: pf.rel, Line: pf.fset.Position(t.Pos()).Line, Confidence: "exact"})
							}
						case *ast.SelectorExpr:
							if base, ok := t.X.(*ast.Ident); ok {
								if imp, ok := pf.imports[base.Name]; ok {
									target := "type:" + imp + "." + t.Sel.Name
									b.AddNode(Node{ID: target, Kind: "type", Name: t.Sel.Name, Qualified: imp + "." + t.Sel.Name, Layer: layerForImport(imp)})
									b.AddEdge(Edge{Kind: "references_type", From: from, To: target, Layer: "code", Source: pf.rel, Line: pf.fset.Position(t.Pos()).Line, Confidence: "exact"})
								}
							}
						}
						return true
					})
				}
			case *ast.FuncDecl:
				caller := "func:" + pf.importPath + "." + x.Name.Name
				recv := receiverName(x.Recv)
				if recv != "" {
					caller = "method:" + pf.importPath + "." + recv + "." + x.Name.Name
					recvType := "type:" + pf.importPath + "." + recv
					if b.HasNode(recvType) {
						b.AddEdge(Edge{Kind: "method_of", From: caller, To: recvType, Layer: "code", Source: pf.rel, Line: pf.fset.Position(x.Pos()).Line, Confidence: "exact"})
					}
				}
				if x.Body == nil {
					continue
				}
				ast.Inspect(x.Body, func(n ast.Node) bool {
					call, ok := n.(*ast.CallExpr)
					if !ok {
						return true
					}
					targetID, targetKind, targetName := resolveCall(call.Fun, pf, decls)
					if targetID != "" {
						b.AddNode(Node{ID: targetID, Kind: targetKind, Name: targetName, Qualified: strings.TrimPrefix(strings.TrimPrefix(targetID, "func:"), "symbol:"), Layer: func() string {
							if targetKind == "external_symbol" {
								return "dependency"
							}
							return "code"
						}()})
						b.AddEdge(Edge{Kind: "calls", From: caller, To: targetID, Layer: "code", Source: pf.rel, Line: pf.fset.Position(call.Pos()).Line, Confidence: "static"})
					}
					return true
				})
			}
		}
	}
}

func resolveCall(expr ast.Expr, pf parsedFile, decls map[string]declarations) (string, string, string) {
	switch x := expr.(type) {
	case *ast.Ident:
		if id, ok := decls[pf.importPath].funcs[x.Name]; ok {
			return id, "function", x.Name
		}
	case *ast.SelectorExpr:
		if base, ok := x.X.(*ast.Ident); ok {
			if imp, ok := pf.imports[base.Name]; ok {
				kind := "function"
				id := "func:" + imp + "." + x.Sel.Name
				if !strings.HasPrefix(imp, "powerfarm.dev/continuity/v2") {
					kind = "external_symbol"
					id = "symbol:" + imp + "." + x.Sel.Name
				}
				return id, kind, x.Sel.Name
			}
		}
	}
	return "", "", ""
}

func receiverName(fl *ast.FieldList) string {
	if fl == nil || len(fl.List) == 0 {
		return ""
	}
	e := fl.List[0].Type
	if p, ok := e.(*ast.StarExpr); ok {
		e = p.X
	}
	if id, ok := e.(*ast.Ident); ok {
		return id.Name
	}
	return exprString(e)
}

func exprString(e ast.Expr) string {
	var buf bytes.Buffer
	_ = printer.Fprint(&buf, token.NewFileSet(), e)
	return buf.String()
}

func firstSentence(s string) string {
	s = strings.TrimSpace(strings.Join(strings.Fields(s), " "))
	if i := strings.Index(s, ". "); i >= 0 {
		return s[:i+1]
	}
	if len(s) > 240 {
		return s[:240]
	}
	return s
}

func layerForImport(imp string) string {
	if strings.HasPrefix(imp, "powerfarm.dev/continuity/v2/") {
		rel := strings.TrimPrefix(imp, "powerfarm.dev/continuity/v2/")
		return layerForPath(rel)
	}
	return "dependency"
}

func layerForPath(rel string) string {
	rel = filepath.ToSlash(rel)
	switch {
	case strings.HasPrefix(rel, "internal/model/"):
		return "domain"
	case strings.HasPrefix(rel, "internal/compiler/"):
		return "compiler"
	case strings.HasPrefix(rel, "internal/effects/"):
		return "effect-semantics"
	case strings.HasPrefix(rel, "internal/runtime/"):
		return "runtime"
	case strings.HasPrefix(rel, "internal/journal/"):
		return "persistence"
	case strings.HasPrefix(rel, "internal/policy/") || strings.HasPrefix(rel, "policy/"):
		return "policy"
	case strings.HasPrefix(rel, "internal/bus/"):
		return "transport"
	case strings.HasPrefix(rel, "internal/engines/") || strings.HasPrefix(rel, "engines/"):
		return "substrate"
	case strings.HasPrefix(rel, "cmd/"):
		return "interface"
	case strings.HasPrefix(rel, "examples/"):
		return "example"
	case strings.HasPrefix(rel, "spec/") || strings.HasPrefix(rel, "docs/"):
		return "specification"
	default:
		return "code"
	}
}

func scanTSV(path string) ([][]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var rows [][]string
	s := bufio.NewScanner(f)
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		rows = append(rows, strings.Split(line, "\t"))
	}
	return rows, s.Err()
}
