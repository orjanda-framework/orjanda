package app

import (
	"context"

	"github.com/orjanda-framework/orjanda/errors"
)

type Definition struct {
	Name         string
	Title        string
	Version      string
	Description  string
	Publisher    string
	Modules      []Module
	Dependencies []Dependency
	// Hooks is the Application's lifecycle init type (TAD §7): a value or
	// pointer to a type implementing Installable, Upgradable, and/or
	// Uninstallable. The Definition struct itself carries no hook methods —
	// site.Install(app.Definition)'s signature is fixed by TAD §7.1 step 1 —
	// so the optional init type rides inside the Definition and is resolved by
	// InstallHook/UpgradeHook/UninstallHook. A nil Hooks (or a type
	// implementing none of the interfaces) makes Install/Upgrade/Uninstall
	// no-ops beyond the framework's own Document/table registration.
	Hooks any
}

type Module struct {
	Name  string
	Title string
}

type Dependency struct {
	App        string
	MinVersion string
}

// Installable defines the hook for app installation.
// Declared with a generic interface to avoid circular dependency at compilation,
// but can be cast to *orjanda.Site when used in the actual runtime.
type Installable interface {
	OnInstall(ctx context.Context, site any) error
}

// Upgradable defines the hook for app upgrades.
type Upgradable interface {
	OnUpgrade(ctx context.Context, site any, fromVersion string) error
}

// Uninstallable defines the hook for app uninstallation.
type Uninstallable interface {
	OnUninstall(ctx context.Context, site any, dropTables bool) error
}

// InstallHook returns the Application's OnInstall hook (PRD §11.3 "Install:
// register documents, run initial migrations, load fixtures"), or nil when its
// Hooks type does not implement Installable.
func (d Definition) InstallHook() Installable {
	h, _ := d.Hooks.(Installable)
	return h
}

// UpgradeHook returns the Application's OnUpgrade hook (PRD §11.3 "Upgrade:
// run pending migrations, execute upgrade hooks"), or nil when its Hooks type
// does not implement Upgradable.
func (d Definition) UpgradeHook() Upgradable {
	h, _ := d.Hooks.(Upgradable)
	return h
}

// UninstallHook returns the Application's OnUninstall hook (PRD §11.3
// "Uninstall: run teardown hooks, optionally drop tables"), or nil when its
// Hooks type does not implement Uninstallable.
func (d Definition) UninstallHook() Uninstallable {
	h, _ := d.Hooks.(Uninstallable)
	return h
}

// ResolveDAG topologically sorts the application definitions based on their dependencies.
// Returns an error if a circular dependency is detected.
func ResolveDAG(apps []Definition) ([]Definition, error) {
	appMap := make(map[string]Definition)
	for i := range apps {
		appMap[apps[i].Name] = apps[i]
	}

	var result []Definition
	visited := make(map[string]int) // 0: unvisited, 1: visiting, 2: visited

	var visit func(name string) error
	visit = func(name string) error {
		state := visited[name]
		if state == 2 {
			return nil
		}
		if state == 1 {
			return errors.New(errors.CodeInternal, "ErrCircularAppDependency: circular dependency detected involving app: "+name, nil, nil)
		}

		visited[name] = 1

		appDef, exists := appMap[name]
		if !exists {
			// If a dependency is not in the list, we assume it's core or not registered yet,
			// which is a validation error.
			return errors.New(errors.CodeValidation, "missing dependency app: "+name, nil, nil)
		}

		for _, dep := range appDef.Dependencies {
			if err := visit(dep.App); err != nil {
				return err
			}
		}

		visited[name] = 2
		result = append(result, appDef)
		return nil
	}

	for i := range apps {
		if err := visit(apps[i].Name); err != nil {
			return nil, err
		}
	}

	return result, nil
}
