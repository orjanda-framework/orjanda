package documents

import (
	orjerrors "github.com/orjanda-framework/orjanda/errors"
	"github.com/orjanda-framework/orjanda/schema"
)

// Department is a business unit; its display name lives on BaseDocument.Name.
type Department struct {
	schema.BaseDocument
	Head        schema.Link `oj:"link=Employee"`     // see PRD §36.1
	Description schema.Text `oj:"label=Description"` // long-form description
}

func (d *Department) DocMeta() schema.Meta {
	return schema.Meta{
		Name:        "Department",
		Module:      "Org",
		Searchable:  true,
		TitleField:  "Name",
		Description: "Business unit within the company",
		Permissions: []schema.DocPermission{
			{Role: "hr_manager", Read: true, Create: true, Write: true, Delete: true},
			{Role: "employee", Read: true},
		},
	}
}

func (d *Department) Get(field string) any {
	switch field {
	case "Head":
		return d.Head
	case "Description":
		return d.Description
	}
	return d.BaseDocument.Get(field)
}

func (d *Department) Set(field string, value any) orjerrors.Error {
	switch field {
	case "Head":
		if v, ok := value.(string); ok {
			d.Head = schema.Link(v)
			return nil
		}
	case "Description":
		if v, ok := value.(string); ok {
			d.Description = schema.Text(v)
			return nil
		}
	}
	return d.BaseDocument.Set(field, value)
}
