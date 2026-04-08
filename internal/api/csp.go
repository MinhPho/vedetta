package api

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"io/fs"
	"sort"
	"strings"
	"sync"

	"golang.org/x/net/html"
)

var (
	cspOnce   sync.Once
	cspHeader string
)

func contentSecurityPolicy() string {
	cspOnce.Do(func() {
		sources := []string{"'self'", "https://unpkg.com"}
		sources = append(sources, inlineScriptHashes()...)
		cspHeader = strings.Join([]string{
			"default-src 'self'",
			"script-src " + strings.Join(sources, " "),
			"script-src-attr 'none'",
			"style-src 'self' 'unsafe-inline'",
			"img-src 'self' data: blob:",
			"media-src 'self' data: blob:",
			"connect-src 'self' ws: wss:",
			"worker-src 'self' blob:",
			"frame-ancestors 'none'",
			"object-src 'none'",
			"base-uri 'self'",
			"form-action 'self'",
		}, "; ")
	})
	return cspHeader
}

func inlineScriptHashes() []string {
	hashes := map[string]struct{}{}
	_ = fs.WalkDir(staticFiles, "static", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".html") {
			return nil
		}
		data, readErr := fs.ReadFile(staticFiles, path)
		if readErr != nil {
			return nil
		}
		for _, hash := range hashInlineScripts(data) {
			hashes[hash] = struct{}{}
		}
		return nil
	})

	result := make([]string, 0, len(hashes))
	for hash := range hashes {
		result = append(result, hash)
	}
	sort.Strings(result)
	return result
}

func hashInlineScripts(data []byte) []string {
	tokenizer := html.NewTokenizer(bytes.NewReader(data))
	var hashes []string

	for {
		switch tokenizer.Next() {
		case html.ErrorToken:
			return hashes
		case html.StartTagToken:
			token := tokenizer.Token()
			if token.Data != "script" {
				continue
			}
			hasSrc := false
			for _, attr := range token.Attr {
				if strings.EqualFold(attr.Key, "src") {
					hasSrc = true
					break
				}
			}
			if hasSrc {
				continue
			}

			var script strings.Builder
			for {
				tt := tokenizer.Next()
				if tt == html.ErrorToken || tt == html.EndTagToken {
					break
				}
				if tt == html.TextToken {
					script.Write(tokenizer.Text())
				}
			}
			text := script.String()
			if strings.TrimSpace(text) == "" {
				continue
			}
			sum := sha256.Sum256([]byte(text))
			hashes = append(hashes, "'sha256-"+base64.StdEncoding.EncodeToString(sum[:])+"'")
		}
	}
}
