// Package core defines the bootstrapped core application for identity, roles, and permissions.
package core

import "github.com/orjanda-framework/orjanda/app"

// App is the app.Definition for Orjanda Core.
// See PRD §11 and TAD §4.
var App = app.Definition{
	Name:        "core",
	Title:       "Orjanda Core",
	Version:     "0.1.0",
	Description: "Core identity, role, and permission documents",
	Publisher:   "Orjanda Framework",
	Modules: []app.Module{
		{Name: "core", Title: "Core"},
	},
}
