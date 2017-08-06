package main

import (
	"flag"
	"fmt"
	"go/build"
	"log"
	"os"
	"sort"
	"strings"
)

var (
	processed = map[string]struct{}{}
	pkgs      map[string]*build.Package
	ids       map[string]int
	nextId    int

	ignored = map[string]bool{
		"C": true,
	}
	ignoredPrefixes []string

	ignoreStdlib   = flag.Bool("s", false, "ignore packages in the Go standard library")
	delveGoroot    = flag.Bool("d", false, "show dependencies of packages in the Go standard library")
	ignorePrefixes = flag.String("p", "", "a comma-separated list of prefixes to ignore")
	ignorePackages = flag.String("i", "", "a comma-separated list of packages to ignore")
	tagList        = flag.String("tags", "", "a comma-separated list of build tags to consider satisified during the build")
	horizontal     = flag.Bool("horizontal", false, "lay out the dependency graph horizontally instead of vertically")
	includeTests   = flag.Bool("t", false, "include test packages")
	unvendor       = flag.Bool("V", false, "strip vendor prefixes from package import names (can help with dangling imports)")

	buildTags    []string
	buildContext = build.Default
)

func main() {
	pkgs = make(map[string]*build.Package)
	ids = make(map[string]int)
	flag.Parse()

	args := flag.Args()
	if len(args) < 1 {
		log.Fatal("need at least one package name to process")
	}

	roots := make(map[string]struct{})
	rootord := make([]string, 0, len(roots))
	for _, pkg := range args {
		if _, ok := roots[pkg]; ok {
			continue
		}
		roots[pkg], rootord = struct{}{}, append(rootord, pkg)
	}
	sort.Strings(rootord)

	if *ignorePrefixes != "" {
		ignoredPrefixes = strings.Split(*ignorePrefixes, ",")
	}
	if *ignorePackages != "" {
		for _, p := range strings.Split(*ignorePackages, ",") {
			ignored[p] = true
		}
	}
	if *tagList != "" {
		buildTags = strings.Split(*tagList, ",")
	}
	buildContext.BuildTags = buildTags

	cwd, err := os.Getwd()
	if err != nil {
		log.Fatalf("failed to get cwd: %s", err)
	}

	for _, pkg := range rootord {
		if err := processPackage(cwd, pkg); err != nil {
			log.Fatal(err)
		}
	}

	fmt.Println("digraph godep {")
	if *horizontal {
		fmt.Println(`rankdir="LR"`)
	}

	// sort packages
	pkgKeys := []string{}
	for k := range pkgs {
		pkgKeys = append(pkgKeys, k)
	}
	sort.Strings(pkgKeys)

	for _, pkgName := range pkgKeys {
		pkg := pkgs[pkgName]
		pkgId := getId(pkgName)

		if isIgnored(pkg) {
			continue
		}

		var color string
		if _, ok := roots[pkg.ImportPath]; ok {
			color = "hotpink1"
		} else if pkg.Goroot {
			color = "palegreen"
		} else if len(pkg.CgoFiles) > 0 {
			color = "darkgoldenrod1"
		} else {
			color = "paleturquoise"
		}

		fmt.Printf("_%d [label=\"%s\" style=\"filled\" color=\"%s\"];\n", pkgId, pkgName, color)

		// Don't render imports from packages in Goroot
		if pkg.Goroot && !*delveGoroot {
			continue
		}

		for _, imp := range getImports(pkg) {
			impPkg := pkgs[imp]
			if impPkg == nil || isIgnored(impPkg) {
				continue
			}

			impId := getId(imp)
			fmt.Printf("_%d -> _%d;\n", pkgId, impId)
		}
	}
	fmt.Println("}")
}

func canonImportPath(pkg *build.Package) string {
	path := pkg.ImportPath
	if !pkg.Goroot && *unvendor {
		const sep = "/vendor/"
		vidx := strings.Index(path, sep)
		if vidx != -1 {
			path = path[vidx+len(sep):]
		}
	}
	return path
}

func processPackage(root string, pkgName string) error {
	if ignored[pkgName] {
		return nil
	}

	pkg, err := buildContext.Import(pkgName, root, 0)
	if err != nil {
		return fmt.Errorf("failed to import %s: %s", pkgName, err)
	}

	if isIgnored(pkg) {
		return nil
	}

	if _, ok := processed[pkg.ImportPath]; ok {
		return nil
	}
	processed[pkg.ImportPath] = struct{}{}

	pkgs[canonImportPath(pkg)] = pkg

	// Don't worry about dependencies for stdlib packages
	if pkg.Goroot && !*delveGoroot {
		return nil
	}

	for _, imp := range getImports(pkg) {
		if _, ok := processed[imp]; !ok {
			if err := processPackage(pkg.Dir, imp); err != nil {
				return err
			}
		}
	}
	return nil
}

func getImports(pkg *build.Package) []string {
	allImports := pkg.Imports
	if *includeTests {
		allImports = append(allImports, pkg.TestImports...)
		allImports = append(allImports, pkg.XTestImports...)
	}
	var imports []string
	found := make(map[string]struct{})
	for _, imp := range allImports {
		if imp == pkg.ImportPath {
			// Don't draw a self-reference when foo_test depends on foo.
			continue
		}
		if _, ok := found[imp]; ok {
			continue
		}
		found[imp] = struct{}{}
		imports = append(imports, imp)
	}
	return imports
}

func getId(name string) int {
	id, ok := ids[name]
	if !ok {
		id = nextId
		nextId++
		ids[name] = id
	}
	return id
}

func hasPrefixes(s string, prefixes []string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

func isIgnored(pkg *build.Package) bool {
	return ignored[pkg.ImportPath] ||
		ignored[canonImportPath(pkg)] ||
		(pkg.Goroot && *ignoreStdlib) ||
		hasPrefixes(pkg.ImportPath, ignoredPrefixes) ||
		hasPrefixes(canonImportPath(pkg), ignoredPrefixes)
}

func debug(args ...interface{}) {
	fmt.Fprintln(os.Stderr, args...)
}

func debugf(s string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, s, args...)
}
