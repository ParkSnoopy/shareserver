package httpx

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
)

var (
	catalogsOnce sync.Once
	catalogs     map[string]map[string]any
)

// loadCatalogs reads local language files once for server-side first paint.
func loadCatalogs() {
	catalogsOnce.Do(func() {
		catalogs = make(map[string]map[string]any, len(languageCodes))
		root, err := os.OpenRoot(repoFile("web", "static", "i18n"))
		if err != nil {
			panic("open language catalogs: " + err.Error())
		}
		defer func() {
			if err := root.Close(); err != nil {
				panic("close language catalogs: " + err.Error())
			}
		}()
		for _, code := range languageCodes {
			body, err := root.ReadFile(code + ".json")
			if err != nil {
				panic("read language catalog: " + err.Error())
			}
			var messages map[string]any
			if err := json.Unmarshal(body, &messages); err != nil {
				panic("parse language catalog: " + err.Error())
			}
			catalogs[code] = messages
		}
	})
}

// T resolves one catalog key and substitutes optional name/value pairs.
func (p pageContext) T(key string, variables ...any) string {
	loadCatalogs()
	text := catalogText(catalogs[p.Language], key)
	if text == "" {
		text = catalogText(catalogs["en"], key)
	}
	if text == "" {
		return key
	}
	for i := 0; i+1 < len(variables); i += 2 {
		name := fmt.Sprint(variables[i])
		text = strings.ReplaceAll(text, "{{"+name+"}}", fmt.Sprint(variables[i+1]))
	}
	return text
}

func catalogText(messages map[string]any, key string) string {
	var value any = messages
	for _, part := range strings.Split(key, ".") {
		node, ok := value.(map[string]any)
		if !ok {
			return ""
		}
		value, ok = node[part]
		if !ok {
			return ""
		}
	}
	text, _ := value.(string)
	return text
}
