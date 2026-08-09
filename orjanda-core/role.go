package core

import (
	orjerrors "github.com/orjanda-framework/orjanda/errors"
	"github.com/orjanda-framework/orjanda/schema"
)

// Role represents a system role definition. See TAD §4.1.
type Role struct {
	schema.BaseDocument
	RoleName string `oj:"required,unique"`
}

func (r *Role) DocMeta() schema.Meta {
	return schema.Meta{
		Name:        "Role",
		Module:      "Core",
		Description: "User role definition",
	}
}

func (r *Role) Get(field string) any {
	if field == "RoleName" {
		return r.RoleName
	}
	return r.BaseDocument.Get(field)
}

func (r *Role) Set(field string, value any) orjerrors.Error {
	if field == "RoleName" {
		if v, ok := value.(string); ok {
			r.RoleName = v
			return nil
		}
	}
	return r.BaseDocument.Set(field, value)
}
