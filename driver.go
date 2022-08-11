package igop

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type ListDriver struct {
	init bool
	root string
	pkgs map[string]string // path -> dir
}

func (d *ListDriver) Lookup(root string, path string) (dir string, found bool) {
	if !d.init || d.root != root {
		d.init = true
		d.root = root
		err := d.Parse(root)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
		}
	}
	dir, found = d.pkgs[path]
	if found {
		return
	}
	var list []string
	for k := range d.pkgs {
		if strings.HasPrefix(path, k+"/") {
			list = append(list, k)
		}
	}
	switch len(list) {
	case 0:
	case 1:
		v := list[0]
		dir, found = filepath.Join(d.pkgs[v], path[len(v+"/"):]), true
	default:
		// check path/v2
		sort.Slice(list, func(i, j int) bool {
			return list[i] > list[j]
		})
		v := list[0]
		dir, found = filepath.Join(d.pkgs[v], path[len(v+"/"):]), true
	}
	return
}

func (d *ListDriver) Parse(root string) error {
	data, err := runGoCommand(root, "list", "-deps", "-e", "-f={{.ImportPath}}={{.Dir}}", ".")
	if err != nil {
		return err
	}
	d.pkgs = make(map[string]string)
	for _, line := range strings.Split(string(data), "\n") {
		pos := strings.Index(line, "=")
		if pos != -1 {
			d.pkgs[line[:pos]] = line[pos+1:]
		}
	}
	return nil
}
