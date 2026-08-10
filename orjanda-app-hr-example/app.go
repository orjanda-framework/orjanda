package main

import (
	"github.com/orjanda-framework/orjanda"
	leavedocs "github.com/orjanda-framework/orjanda-app-hr-example/modules/leave/documents"
	orgdocs "github.com/orjanda-framework/orjanda-app-hr-example/modules/org/documents"
	core "github.com/orjanda-framework/orjanda/orjanda-core"
	"github.com/orjanda-framework/orjanda/schema"
)

func configure(site *orjanda.Site) error {
	site.Install(core.App)
	reg := site.Registry
	coreDocs := []schema.Document{&core.User{}, &core.Role{}, &core.RolePermission{}}
	if err := registerDocs(reg, "core", coreDocs); err != nil {
		return err
	}
	userDocs := []schema.Document{
		&orgdocs.Department{},
		&orgdocs.Employee{},
		&leavedocs.LeaveType{},
		&leavedocs.LeaveRequest{},
	}
	if err := registerDocs(reg, "hr", userDocs); err != nil {
		return err
	}
	appSite = site
	registerHooks(site)
	return nil
}

func registerDocs(reg schema.Registry, appName string, docs []schema.Document) error {
	for _, d := range docs {
		if err := reg.Register(appName, d); err != nil {
			return err
		}
	}
	return nil
}
