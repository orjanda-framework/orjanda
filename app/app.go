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

// ResolveDAG topologically sorts the application definitions based on their dependencies.
// Returns an error if a circular dependency is detected.
func ResolveDAG(apps []Definition) ([]Definition, error) {
	appMap := make(map[string]Definition)
	for _, app := range apps {
		appMap[app.Name] = app
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

	for _, app := range apps {
		if err := visit(app.Name); err != nil {
			return nil, err
		}
	}

	return result, nil
}
