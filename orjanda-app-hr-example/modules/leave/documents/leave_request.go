package documents

import (
	"time"

	orjerrors "github.com/orjanda-framework/orjanda/errors"
	"github.com/orjanda-framework/orjanda/schema"
)

// LeaveRequest is an employee's leave application. Lifecycle (status) is
// governed by the LeaveApproval workflow, which seeds WorkflowState to "Draft"
// on insert via the app's before_save hook. The field is declared here so the
// column exists at table creation; workflow.Engine.Register detects it and
// does not add a duplicate (TAD §8.1 step 1).
type LeaveRequest struct {
	schema.BaseDocument
	Employee      schema.Link `oj:"required,link=Employee,searchable"`
	LeaveType     schema.Link `oj:"required,link=LeaveType,searchable"`
	FromDate      schema.Date `oj:"required"`
	ToDate        schema.Date `oj:"required"`
	Reason        schema.Text `oj:"required,label=Reason"`
	WorkflowState string
}

func (l *LeaveRequest) DocMeta() schema.Meta {
	return schema.Meta{
		Name:        "LeaveRequest",
		Module:      "Leave",
		Submittable: true,
		Searchable:  true,
		TitleField:  "Name",
		Description: "Employee leave application",
		Permissions: []schema.DocPermission{
			{Role: "hr_manager", Read: true, Create: true, Write: true, Delete: true, Submit: true},
			{Role: "department_head", Read: true, Write: true, Submit: true},
			{Role: "employee", Read: true, Create: true, Submit: true},
		},
	}
}

func (l *LeaveRequest) Get(field string) any {
	switch field {
	case "Employee":
		return l.Employee
	case "LeaveType":
		return l.LeaveType
	case "FromDate":
		return l.FromDate
	case "ToDate":
		return l.ToDate
	case "Reason":
		return l.Reason
	case "WorkflowState":
		return l.WorkflowState
	}
	return l.BaseDocument.Get(field)
}

func (l *LeaveRequest) Set(field string, value any) orjerrors.Error {
	switch field {
	case "Employee":
		if v, ok := value.(string); ok {
			l.Employee = schema.Link(v)
			return nil
		}
	case "LeaveType":
		if v, ok := value.(string); ok {
			l.LeaveType = schema.Link(v)
			return nil
		}
	case "FromDate":
		if v, ok := value.(time.Time); ok {
			l.FromDate = schema.Date(v)
			return nil
		}
	case "ToDate":
		if v, ok := value.(time.Time); ok {
			l.ToDate = schema.Date(v)
			return nil
		}
	case "Reason":
		if v, ok := value.(string); ok {
			l.Reason = schema.Text(v)
			return nil
		}
	case "WorkflowState":
		if v, ok := value.(string); ok {
			l.WorkflowState = v
			return nil
		}
	}
	return l.BaseDocument.Set(field, value)
}
