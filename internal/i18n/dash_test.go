// SPDX-License-Identifier: Apache-2.0
package i18n

import (
	"go/scanner"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The em dash stands in for a colon, a comma or a full stop in machine-made
// text and reads as filler, so nothing a person reads in maply has one: not
// a translation, not a string in the Go code, not the Mini App.
const emDash = "—"

func TestNoEmDashInTranslations(t *testing.T) {
	for _, key := range Keys() {
		for _, code := range Codes() {
			if s := translations[key][code]; strings.Contains(s, emDash) {
				t.Errorf("%s [%s] has an em dash: %q", key, code, s)
			}
		}
	}
}

func TestNoEmDashInGoStrings(t *testing.T) {
	for _, dir := range []string{"../../internal", "../../cmd"} {
		err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return err
			}
			src, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			fset := token.NewFileSet()
			file := fset.AddFile(path, -1, len(src))
			var s scanner.Scanner
			s.Init(file, src, nil, 0)
			for {
				pos, tok, lit := s.Scan()
				if tok == token.EOF {
					break
				}
				if (tok == token.STRING || tok == token.CHAR) && strings.Contains(lit, emDash) {
					t.Errorf("%s: a string has an em dash: %s", fset.Position(pos), lit)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestNoEmDashInMiniApp(t *testing.T) {
	err := filepath.WalkDir("../../web/app", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for i, line := range strings.Split(string(src), "\n") {
			if strings.Contains(line, emDash) {
				t.Errorf("%s:%d has an em dash", path, i+1)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
