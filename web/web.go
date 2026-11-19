// Package web guarda a página index.html dentro do executável, para o
// servidor -http entregá-la em /.
package web

import _ "embed"

//go:embed index.html
var Index []byte
