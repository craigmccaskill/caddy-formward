package formward

import (
	"github.com/caddyserver/caddy/v2"
)

func init() {
	caddy.RegisterModule(Module{})
}

type Module struct{}

func (Module) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID:  "http.handlers.formward",
		New: func() caddy.Module { return new(Module) },
	}
}
