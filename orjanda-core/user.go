package core

import (
	orjerrors "github.com/orjanda-framework/orjanda/errors"
	"github.com/orjanda-framework/orjanda/schema"
)

// User represents a system user account. See TAD §4.1.
type User struct {
	schema.BaseDocument
	Email    string     `oj:"required,unique,format=email,searchable"`
	FullName string     `oj:"required,searchable"`
	Password string     `oj:"hidden"`
	Roles    []UserRole `oj:"child_table"`
	Active   bool       `oj:"default=true"`
}

func (u *User) DocMeta() schema.Meta {
	return schema.Meta{
		Name:        "User",
		Module:      "Core",
		Description: "System user account",
	}
}

func (u *User) Get(field string) any {
	switch field {
	case "Email":
		return u.Email
	case "FullName":
		return u.FullName
	case "Password":
		return u.Password
	case "Active":
		return u.Active
	}
	return u.BaseDocument.Get(field)
}

func (u *User) Set(field string, value any) orjerrors.Error {
	switch field {
	case "Email":
		if v, ok := value.(string); ok {
			u.Email = v
			return nil
		}
	case "FullName":
		if v, ok := value.(string); ok {
			u.FullName = v
			return nil
		}
	case "Password":
		if v, ok := value.(string); ok {
			u.Password = v
			return nil
		}
	case "Active":
		if v, ok := value.(bool); ok {
			u.Active = v
			return nil
		}
	}
	return u.BaseDocument.Set(field, value)
}

// UserRole represents a role assigned to a User (child table). See TAD §4.1.
type UserRole struct {
	schema.BaseChild
	Role schema.Link `oj:"link=Role,required"`
}

func (ur *UserRole) DocMeta() schema.Meta {
	return schema.Meta{
		Name: "UserRole",
	}
}

func (ur *UserRole) Get(field string) any {
	if field == "Role" {
		return string(ur.Role)
	}
	return ur.BaseChild.Get(field)
}

func (ur *UserRole) Set(field string, value any) orjerrors.Error {
	if field == "Role" {
		if v, ok := value.(string); ok {
			ur.Role = schema.Link(v)
			return nil
		}
		if v, ok := value.(schema.Link); ok {
			ur.Role = v
			return nil
		}
	}
	return ur.BaseChild.Set(field, value)
}
