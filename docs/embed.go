package docs

import "embed"

//go:embed all:.vitepress/dist
var Assets embed.FS
