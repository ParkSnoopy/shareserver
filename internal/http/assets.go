package httpx

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"html/template"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

var (
	scriptTag = regexp.MustCompile(`(?i)<script\b([^>]*)>`)
	// A plain URL or a {{static "..."}} call inside the attribute.
	srcAttr    = regexp.MustCompile(`\bsrc=['"](?:\{\{static\s+")?(/static/[^"']+)`)
	moduleType = regexp.MustCompile(`\btype="module"`)
	// Static imports and re-exports, side-effect imports and literal dynamic
	// imports. Group 1 is the specifier.
	importPatterns = []*regexp.Regexp{
		regexp.MustCompile(`\b(?:import|export)\b[^"'();]*?\bfrom\s*["']([^"']+)["']`),
		regexp.MustCompile(`\bimport\s*["']([^"']+)["']`),
		regexp.MustCompile(`\bimport\(\s*["']([^"']+)["']\s*\)`),
	}
)

// immutableAge is the cache lifetime of a versioned static URL. The URL
// changes with the content, so the browser never needs to revalidate it.
const immutableAge = "public, max-age=31536000, immutable"

// staticFiles holds every file under web/static in memory, keyed by URL path.
// Each file has a content version, and JavaScript imports of other static
// files are rewritten to carry it (?v=), so a versioned URL always names the
// same bytes, including everything it imports.
type staticFiles map[string]*staticFile

type staticFile struct {
	name    string // base name, selects the content type
	body    []byte
	version string
	imports []string // static files that this module imports
}

// loadStaticFiles reads web/static once at startup. Edits to static files
// need a restart, as edits to templates already do.
func loadStaticFiles(root string) staticFiles {
	raw := map[string][]byte{}
	err := filepath.WalkDir(root, func(file string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		body, err := os.ReadFile(file)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, file)
		if err != nil {
			return err
		}
		raw["/static/"+filepath.ToSlash(rel)] = body
		return nil
	})
	if err != nil {
		panic(err)
	}

	files := staticFiles{}
	visiting := map[string]bool{}
	var load func(url string) *staticFile
	load = func(url string) *staticFile {
		if file := files[url]; file != nil {
			return file
		}
		body, ok := raw[url]
		// An import cycle keeps the unversioned specifier.
		if !ok || visiting[url] {
			return nil
		}
		visiting[url] = true
		defer delete(visiting, url)

		file := &staticFile{name: path.Base(url)}
		if strings.HasSuffix(url, ".js") || strings.HasSuffix(url, ".mjs") {
			body = rewriteImports(body, func(specifier string) string {
				target := resolveImport(url, specifier)
				dependency := load(target)
				if dependency == nil {
					return specifier
				}
				file.imports = append(file.imports, target)
				return specifier + "?v=" + dependency.version
			})
		}
		sum := sha256.Sum256(body)
		file.body = body
		file.version = hex.EncodeToString(sum[:6])
		files[url] = file
		return file
	}
	for url := range raw {
		load(url)
	}
	return files
}

// rewriteImports replaces each import specifier with rewrite(specifier).
func rewriteImports(body []byte, rewrite func(string) string) []byte {
	source := string(body)
	for _, pattern := range importPatterns {
		source = pattern.ReplaceAllStringFunc(source, func(match string) string {
			group := pattern.FindStringSubmatchIndex(match)
			return match[:group[2]] + rewrite(match[group[2]:group[3]]) + match[group[3]:]
		})
	}
	return []byte(source)
}

// resolveImport returns the /static/ URL of a relative or root-relative
// specifier, or "" for a bare or external specifier.
func resolveImport(from, specifier string) string {
	var target string
	switch {
	case strings.HasPrefix(specifier, "/static/"):
		target = path.Clean(specifier)
	case strings.HasPrefix(specifier, "./"), strings.HasPrefix(specifier, "../"):
		target = path.Join(path.Dir(from), specifier)
	}
	if !strings.HasPrefix(target, "/static/") {
		return ""
	}
	return target
}

// url returns the versioned URL of a static file. Templates use it as
// {{static "/static/..."}}.
func (s staticFiles) url(name string) string {
	if file := s[name]; file != nil {
		return name + "?v=" + file.version
	}
	return name
}

// ServeHTTP serves a static file. A request for the current version may be
// cached without revalidation. Any other request (no version, or the version
// of an older deployment) must revalidate, so a browser never keeps a module
// whose imports no longer match: an old progress.js beside a new share.js
// once failed with "progress.state is not a function".
func (s staticFiles) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	file := s[r.URL.Path]
	if file == nil {
		http.NotFound(w, r)
		return
	}
	if r.URL.Query().Get("v") == file.version {
		w.Header().Set("Cache-Control", immutableAge)
	} else {
		w.Header().Set("Cache-Control", "no-cache, must-revalidate")
	}
	w.Header().Set("ETag", `"`+file.version+`"`)
	http.ServeContent(w, r, file.name, time.Time{}, bytes.NewReader(file.body))
}

// siteAssets returns the space-separated versioned URLs that prefetch.js
// loads into the browser cache once a page has loaded: every module that
// some template loads, with everything it imports, and every language
// catalog. The URLs are served as immutable, so after the first page every
// other page and a language switch load them from the cache without a
// request. Waiting for the load event keeps this traffic from slowing the
// first page: HTTP/2 servers in Go, Caddy included, do not prioritize
// streams, so early prefetches would share bandwidth with the page itself.
func siteAssets(t *template.Template, files staticFiles) string {
	assets := map[string]bool{}
	for _, tmpl := range t.Templates() {
		if tmpl.Tree == nil || tmpl.Tree.Root == nil {
			continue
		}
		for _, tag := range scriptTag.FindAllStringSubmatch(tmpl.Tree.Root.String(), -1) {
			if src := srcAttr.FindStringSubmatch(tag[1]); src != nil && moduleType.MatchString(tag[1]) {
				collectModule(src[1], files, assets)
			}
		}
	}
	for url := range files {
		if strings.HasPrefix(url, "/static/i18n/") && strings.HasSuffix(url, ".json") {
			assets[url] = true
		}
	}
	urls := make([]string, 0, len(assets))
	for url := range assets {
		urls = append(urls, files.url(url))
	}
	sort.Strings(urls)
	return strings.Join(urls, " ")
}

// collectModule adds a module and everything it imports, recursively.
func collectModule(url string, files staticFiles, assets map[string]bool) {
	file := files[url]
	if file == nil || assets[url] {
		return
	}
	assets[url] = true
	for _, target := range file.imports {
		collectModule(target, files, assets)
	}
}
