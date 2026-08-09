package core

import (
	orjerrors "github.com/orjanda-framework/orjanda/errors"
	"github.com/orjanda-framework/orjanda/schema"
)

// RolePermission represents a granted set of CRUD/Submit permissions for a Role on a DocType.
// See TAD §4.1.
type RolePermission struct {
	schema.BaseDocument
	Role    schema.Link `oj:"link=Role,required"`
	DocType string      `oj:"required"`
	Read    bool
	Write   bool
	Create  bool
	Delete  bool
	Submit  bool
}

func (rp *RolePermission) DocMeta() schema.Meta {
	return schema.Meta{
		Name:        "RolePermission",
		Module:      "Core",
		Description: "Role permission grant per DocType",
	}
}

func (rp *RolePermission) Get(field string) any {
	switch field {
	case "Role":
		return string(rp.Role)
	case "DocType":
		return rp.DocType
	case "Read":
		return rp.Read
	case "Write":
		return rp.Write
	case "Create":
		return rp.Create
	case "Delete":
		return rp.Delete
	case "Submit":
		return rp.Submit
	}
	return rp.BaseDocument.Get(field)
}

func (rp *RolePermission) Set(field string, value any) orjerrors.Error {
	switch field {
	case "Role":
		if v, ok := value.(string); ok {
			rp.Role = schema.Link(v)
			return nil
		}
		if v, ok := value.(schema.Link); ok {
			rp.Role = v
			return nil
		}
	case "DocType":
		if v, ok := value.(string); ok {
			rp.DocType = v
			return nil
		}
	case "Read":
		if v, ok := value.(bool); ok {
			rp.Read = v
			return nil
		}
	case "Write":
		if v, ok := value.(bool); ok {
			rp.Write = v
			return nil
		}
	case "Create":
		if v, ok := value.(bool); ok {
			rp.Create = v
			return nil
		}
	case "Delete":
		if v, ok := value.(bool); ok {
			rp.Delete = v
			return nil
		}
	case "Submit":
		if v, ok := value.(bool); ok {
			rp.Submit = v
			return nil
		}
	}
	return rp.BaseDocument.Set(field, value)
}
