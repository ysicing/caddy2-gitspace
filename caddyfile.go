package caddy2k8s

import (
	"encoding/json"

	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"github.com/caddyserver/caddy/v2/caddyconfig/httpcaddyfile"
)

func init() {
	httpcaddyfile.RegisterGlobalOption("k8s_router", parseK8sRouterOption)
}

// parseK8sRouterOption parses the k8s_router global option from the Caddyfile
func parseK8sRouterOption(d *caddyfile.Dispenser, _ interface{}) (interface{}, error) {
	kr := new(K8sRouter)
	if err := kr.UnmarshalCaddyfile(d); err != nil {
		return nil, err
	}

	// Marshal to JSON to create RawMessage
	b, err := json.Marshal(kr)
	if err != nil {
		return nil, err
	}

	return httpcaddyfile.App{
		Name:  "k8s_router",
		Value: json.RawMessage(b),
	}, nil
}
