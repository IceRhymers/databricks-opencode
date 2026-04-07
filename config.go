package main

import (
	"log"

	"github.com/IceRhymers/databricks-opencode/pkg/jsonconfig"
)

// EnsureConfig is an idempotent config writer. It checks whether
// config.json already points at proxyURL and only calls Patch when needed.
// No backup, no restore — the config persists pointing at the fixed port.
func EnsureConfig(c *jsonconfig.Config, proxyURL, model, apiKey string, forceModel bool) error {
	if !c.NeedsConfig(proxyURL) {
		log.Printf("databricks-opencode: config.json already configured for %s", proxyURL)
		return nil
	}
	log.Printf("databricks-opencode: writing config.json for %s", proxyURL)
	return c.Patch(proxyURL, model, apiKey, forceModel)
}
