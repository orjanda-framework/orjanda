package documents

import (
	orjerrors "github.com/orjanda-framework/orjanda/errors"
	"github.com/orjanda-framework/orjanda/schema"
)

// LeaveType is a category of leave; display name lives on BaseDocument.Name.
type LeaveType struct {
	schema.BaseDocument
	MaxDaysPerYear int  `oj:"required,label=Max Days Per Year"`
	IsPaid         bool `oj:"label=Paid Leave"`
}

func (l *LeaveType) DocMeta() schema.Meta {
	return schema.Meta{
		Name:        "LeaveType",
		Module:      "Leave",
		Searchable:  true,
		TitleField:  "Name",
		Description: "Category of leave",
		Permissions: []schema.DocPermission{
			{Role: "hr_manager", Read: true, Create: true, Write: true, Delete: true},
			{Role: "employee", Read: true},
		},
	}
}

func (l *LeaveType) Get(field string) any {
	switch field {
	case "MaxDaysPerYear":
		return l.MaxDaysPerYear
	case "IsPaid":
		return l.IsPaid
	}
	return l.BaseDocument.Get(field)
}

func (l *LeaveType) Set(field string, value any) orjerrors.Error {
	switch field {
	case "MaxDaysPerYear":
		if v, ok := value.(int); ok {
			l.MaxDaysPerYear = v
			return nil
		}
	case "IsPaid":
		if v, ok := value.(bool); ok {
			l.IsPaid = v
			return nil
		}
	}
	return l.BaseDocument.Set(field, value)
}
