package documents

import (
	"time"

	orjerrors "github.com/orjanda-framework/orjanda/errors"
	"github.com/orjanda-framework/orjanda/schema"
)

// Employee is a company employee. Salary is gated to hr_manager at the field
// level (oj:"permission=", PRD §16.2) — the Phase 12 field-gating check.
type Employee struct {
	schema.BaseDocument
	FirstName  string          `oj:"required,searchable"`
	LastName   string          `oj:"required,searchable"`
	Email      string          `oj:"required,unique,format=email,searchable"`
	Department schema.Link     `oj:"link=Department,searchable"`
	Status     string          `oj:"options=Active|On Leave|Terminated,default=Active"`
	JoinDate   schema.Date     `oj:"required"`
	Salary     schema.Currency `oj:"permission=hr_manager,precision=2"`
}

func (e *Employee) DocMeta() schema.Meta {
	return schema.Meta{
		Name:        "Employee",
		Module:      "Org",
		Searchable:  true,
		TitleField:  "Name",
		Description: "Company employee",
		Permissions: []schema.DocPermission{
			{Role: "hr_manager", Read: true, Create: true, Write: true, Delete: true},
			{Role: "department_head", Read: true},
			{Role: "employee", Read: true},
		},
	}
}

func (e *Employee) Get(field string) any {
	switch field {
	case "FirstName":
		return e.FirstName
	case "LastName":
		return e.LastName
	case "Email":
		return e.Email
	case "Department":
		return e.Department
	case "Status":
		return e.Status
	case "JoinDate":
		return e.JoinDate
	case "Salary":
		return e.Salary
	}
	return e.BaseDocument.Get(field)
}

func (e *Employee) Set(field string, value any) orjerrors.Error {
	switch field {
	case "FirstName":
		if v, ok := value.(string); ok {
			e.FirstName = v
			return nil
		}
	case "LastName":
		if v, ok := value.(string); ok {
			e.LastName = v
			return nil
		}
	case "Email":
		if v, ok := value.(string); ok {
			e.Email = v
			return nil
		}
	case "Department":
		if v, ok := value.(string); ok {
			e.Department = schema.Link(v)
			return nil
		}
	case "Status":
		if v, ok := value.(string); ok {
			e.Status = v
			return nil
		}
	case "JoinDate":
		if v, ok := value.(time.Time); ok {
			e.JoinDate = schema.Date(v)
			return nil
		}
		if v, ok := value.(string); ok {
			if t, err := time.Parse("2006-01-02", v); err == nil {
				e.JoinDate = schema.Date(t)
				return nil
			}
		}
	case "Salary":
		if v, ok := value.(float64); ok {
			e.Salary = schema.Currency(v)
			return nil
		}
	}
	return e.BaseDocument.Set(field, value)
}
