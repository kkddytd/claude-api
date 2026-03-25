package frontend

import "embed"

// StaticFiles 嵌入所有前端静态文件
//
//go:embed *.html img/* css/*.css js/*.js
var StaticFiles embed.FS
