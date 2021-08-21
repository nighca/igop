package interp

import (
	"errors"
	"flag"
	"fmt"
	"go/ast"
	"go/build"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/goplus/reflectx"
	"golang.org/x/tools/go/loader"
	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/go/ssa"
	"golang.org/x/tools/go/ssa/ssautil"
)

var (
	UnsafeSizes types.Sizes
)

func loadFile2(input string, src interface{}) (*ssa.Package, error) {
	if !filepath.IsAbs(input) {
		wd, _ := os.Getwd()
		input = filepath.Join(wd, input)
	}
	const mode = parser.AllErrors | parser.ParseComments
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, input, src, mode)
	if err != nil {
		return nil, err
	}
	cfg := &loader.Config{}
	cfg.Fset = fset
	cfg.CreateFromFiles(input, f)
	iprog, err := cfg.Load()
	if err != nil {
		return nil, err
	}
	prog := ssautil.CreateProgram(iprog, ssa.SanityCheckFunctions)
	prog.Build()
	var mainPkg *ssa.Package
	if len(iprog.Created) > 0 {
		mainPkg = prog.Package(iprog.Created[0].Pkg)
	} else {
		if pkgs := ssautil.MainPackages(prog.AllPackages()); len(pkgs) > 0 {
			mainPkg = pkgs[0]
		}
	}
	if mainPkg == nil {
		return nil, errors.New("not found main package")
	}
	return mainPkg, nil
}

func loadFile(input string, src interface{}) (*ssa.Package, error) {
	if !filepath.IsAbs(input) {
		wd, _ := os.Getwd()
		input = filepath.Join(wd, input)
	}
	const mode = parser.AllErrors | parser.ParseComments
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, input, src, mode)
	if err != nil {
		return nil, err
	}
	var hasOtherPkgs bool
	for _, im := range f.Imports {
		v, _ := strconv.Unquote(im.Path.Value)
		if !externPackages[v] {
			hasOtherPkgs = true
			break
		}
	}
	if !hasOtherPkgs {
		pkg := types.NewPackage("main", "")
		ssapkg, _, err := ssautil.BuildPackage(&types.Config{Importer: importer.Default()}, fset, pkg, []*ast.File{f}, ssa.SanityCheckFunctions)
		if err != nil {
			return nil, err
		}
		ssapkg.Build()
		return ssapkg, nil
	}
	cfg := &packages.Config{
		Fset: fset,
		Mode: packages.NeedName | packages.NeedDeps | packages.LoadTypes | packages.NeedSyntax | packages.NeedTypesInfo | packages.NeedTypesSizes,
	}
	cfg.ParseFile = func(fset *token.FileSet, filename string, src []byte) (*ast.File, error) {
		if filename == input {
			return f, nil
		}
		return parser.ParseFile(fset, filename, src, mode)
	}
	list, err := packages.Load(cfg, input)
	if err != nil {
		return nil, err
	}
	list[0].ID = "main"
	list[0].PkgPath = "main"
	// hack fix types.Types.Path() command-line-arguments
	v := reflect.ValueOf(list[0].Types).Elem()
	reflectx.Field(v, 0).SetString("main")
	prog, pkgs := ssautil.AllPackages(list, ssa.SanityCheckFunctions)
	prog.Build()
	mainPkgs := ssautil.MainPackages(pkgs)
	if len(mainPkgs) == 0 {
		return nil, errors.New("not found main package")
	}
	return mainPkgs[0], nil
}

func loadPkg(input string) (*ssa.Package, error) {
	wd, _ := os.Getwd()
	p, err := build.Import(input, wd, 0)
	if err != nil {
		return nil, err
	}
	var hasOtherPkgs bool
	for _, im := range p.Imports {
		if !externPackages[im] {
			hasOtherPkgs = true
			break
		}
	}
	if !hasOtherPkgs {
		const mode = parser.AllErrors | parser.ParseComments
		fset := token.NewFileSet()
		var files []*ast.File
		for _, fname := range p.GoFiles {
			filename := filepath.Join(p.Dir, fname)
			f, err := parser.ParseFile(fset, filename, nil, mode)
			if err != nil {
				return nil, err
			}
			files = append(files, f)
		}
		pkg := types.NewPackage("main", "")
		ssapkg, _, err := ssautil.BuildPackage(&types.Config{Importer: importer.Default()}, fset, pkg, files, ssa.SanityCheckFunctions)
		if err != nil {
			return nil, err
		}
		ssapkg.Build()
		return ssapkg, nil
	}
	cfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedDeps | packages.LoadTypes | packages.NeedSyntax | packages.NeedTypesInfo | packages.NeedTypesSizes,
	}
	list, err := packages.Load(cfg, input)
	if err != nil {
		return nil, err
	}
	prog, pkgs := ssautil.AllPackages(list, ssa.SanityCheckFunctions)
	prog.Build()
	mainPkgs := ssautil.MainPackages(pkgs)
	if len(mainPkgs) == 0 {
		return nil, errors.New("not found main package")
	}
	return mainPkgs[0], nil
}

func Run(mode Mode, input string, args []string) error {
	if strings.HasSuffix(input, ".go") {
		return RunFile(mode, input, nil, args)
	}
	pkg, err := loadPkg(input)
	if err != nil {
		return err
	}
	return RunPkg(pkg, mode, input, "main", args)
}

func foundPkg(pkg string) (*build.Package, error) {
	if filepath.IsAbs(pkg) {
		return build.ImportDir(pkg, build.FindOnly)
	} else {
		return build.Import(pkg, ".", build.FindOnly)
	}
}

func RunTest(mode Mode, input string, args []string) error {
	p, err := foundPkg(input)
	if err != nil {
		return fmt.Errorf("not found pkg: %v", err)
	}
	if p.Dir != "." {
		os.Chdir(p.Dir)
	}
	pkgpath, pkgs, err := LoadTest(".")
	if err != nil {
		return err
	}
	if len(pkgs) == 0 {
		fmt.Printf("?\t%s [no test files]\n", pkgpath)
		return nil
	}
	RunTestPkg(pkgs, mode, pkgpath, args)
	return nil
}

func RunFile(mode Mode, filename string, src interface{}, args []string) error {
	pkg, err := loadFile(filename, src)
	if err != nil {
		return err
	}
	return RunPkg(pkg, mode, filename, "main", args)
}

func LoadTest(input string) (string, []*ssa.Package, error) {
	cfg := &packages.Config{
		Mode:  packages.NeedName | packages.NeedDeps | packages.LoadTypes | packages.NeedSyntax | packages.NeedTypesInfo | packages.NeedTypesSizes,
		Tests: true,
	}
	list, err := packages.Load(cfg, input)
	if err != nil {
		return "", nil, err
	}
	prog, ppkgs := ssautil.AllPackages(list, ssa.SanityCheckFunctions)
	prog.Build()
	var pkgs []*ssa.Package
	for _, p := range ppkgs {
		if p == nil {
			continue
		}
		pkgs = append(pkgs, p)
	}
	if len(pkgs) == 0 {
		return "", nil, errors.New("not found package")
	}
	return pkgs[0].Pkg.Path(), pkgs, nil
}

func RunTestPkg(pkgs []*ssa.Package, mode Mode, input string, args []string) {
	var testPkgs []*ssa.Package
	for _, pkg := range pkgs {
		if p := pkg.Prog.CreateTestMainPackage(pkg); p != nil {
			testPkgs = append(testPkgs, p)
		}
	}
	os.Args = []string{input}
	if args != nil {
		os.Args = append(os.Args, args...)
	}
	var failed bool
	start := time.Now()
	defer func() {
		sec := time.Since(start).Seconds()
		if failed {
			fmt.Printf("FAIL\t%s %0.3fs\n", input, sec)
		} else {
			fmt.Printf("ok\t%s %0.3fs\n", input, sec)
		}
	}()
	if len(testPkgs) == 0 {
		fmt.Println("testing: warning: no tests to run")
	}
	for _, pkg := range testPkgs {
		flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)
		if code := Interpret(pkg, mode, "main"); code != 0 {
			failed = true
		}
	}
}

func RunPkg(mainPkg *ssa.Package, mode Mode, input string, entry string, args []string) error {
	// reset os args and flag
	os.Args = []string{input}
	if args != nil {
		os.Args = append(os.Args, args...)
	}
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)

	exitCode := Interpret(mainPkg, mode, entry)
	if exitCode != 0 {
		return fmt.Errorf("interpreting %v: exit code was %d", input, exitCode)
	}
	return nil
}
