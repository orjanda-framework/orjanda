package schema

import (
	"fmt"
	"reflect"
	"sync"

	"github.com/orjanda-framework/orjanda/errors"
)

type registry struct {
	mu         sync.RWMutex
	docs       map[string]*CompiledDoc
	registered []registeredDoc
	compiled   bool
	appDeps    map[string][]string // app name -> dependencies
}

type registeredDoc struct {
	app string
	doc Document
}

// NewRegistry creates a new, uncompiled schema.Registry instance.
func NewRegistry() Registry {
	return &registry{
		docs:    make(map[string]*CompiledDoc),
		appDeps: make(map[string][]string),
	}
}

// RegisterDependency is a package-level helper that allows recording dependencies
// between applications in the registry, to verify dependency-ordered registration.
func RegisterDependency(reg Registry, appName string, dependency string) {
	if r, ok := reg.(*registry); ok {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.appDeps[appName] = append(r.appDeps[appName], dependency)
	}
}

func (r *registry) Register(app string, doc Document) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.compiled {
		return errors.New(errors.CodeConflict, "cannot register document: registry is compiled and locked", nil, nil)
	}

	meta := doc.DocMeta()
	if meta.Name == "" {
		return errors.New(errors.CodeValidation, "document Meta.Name cannot be empty", nil, nil)
	}

	r.registered = append(r.registered, registeredDoc{
		app: app,
		doc: doc,
	})

	return nil
}

func (r *registry) Compile() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.compiled {
		return errors.New(errors.CodeConflict, "registry is already compiled", nil, nil)
	}

	// 1. Enforce App-Dependency-Ordered Registration
	appFirstIndex := make(map[string]int)
	for idx, rd := range r.registered {
		if _, ok := appFirstIndex[rd.app]; !ok {
			appFirstIndex[rd.app] = idx
		}
	}

	for idx, rd := range r.registered {
		app := rd.app
		deps := r.appDeps[app]
		for _, dep := range deps {
			if depIdx, ok := appFirstIndex[dep]; ok {
				if depIdx > idx {
					return errors.New(errors.CodeValidation, fmt.Sprintf("app-dependency-ordered registration violation: app %q depends on %q, but %q documents were registered after %q", app, dep, dep, app), nil, nil)
				}
			}
		}
	}

	// 2. Reflect and Compile Documents
	for _, rd := range r.registered {
		doc := rd.doc
		meta := doc.DocMeta()

		// Duplicate check
		if _, dup := r.docs[meta.Name]; dup {
			return errors.New(errors.CodeValidation, fmt.Sprintf("duplicate Document name registered: %q", meta.Name), nil, nil)
		}

		t := reflect.TypeOf(doc)
		fields, children, err := ParseFields(t)
		if err != nil {
			return err
		}

		// Prepend BaseDocument fields (PRD §10.2)
		baseFields := getBaseDocumentFields()
		allFields := append(baseFields, fields...)

		// For each child table, prepend BaseChild fields
		for i := range children {
			childBase := getBaseChildFields()
			children[i].Fields = append(childBase, children[i].Fields...)
		}

		sortOrder := Ascending
		if meta.SortOrder == Descending {
			sortOrder = Descending
		}

		compiled := &CompiledDoc{
			Name:        meta.Name,
			App:         rd.app,
			Module:      meta.Module,
			TableName:   camelToSnake(meta.Name) + "s", // Pluralize table name as per TAD §1.4
			Searchable:  meta.Searchable,
			Submittable: meta.Submittable,
			Icon:        meta.Icon,
			Description: meta.Description,
			TitleField:  meta.TitleField,
			SortField:   meta.SortField,
			SortOrder:   sortOrder,
			Fields:      allFields,
			Permissions: meta.Permissions,
			ChildTables: children,
		}

		r.docs[meta.Name] = compiled
	}

	// 3. Relationship Resolution Pass (verify all Links point to registered types)
	for _, doc := range r.docs {
		// Check top-level fields
		for _, f := range doc.Fields {
			if f.Type == FieldTypeLink {
				if f.LinkTarget == "" {
					return errors.New(errors.CodeValidation, fmt.Sprintf("Link field %q in %q lacks a link target", f.Name, doc.Name), nil, nil)
				}
				if _, exists := r.docs[f.LinkTarget]; !exists {
					return errors.New(errors.CodeValidation, fmt.Sprintf("Link field %q in %q references non-existent DocType %q", f.Name, doc.Name, f.LinkTarget), nil, nil)
				}
			}
		}
		// Check child table fields
		for _, child := range doc.ChildTables {
			for _, f := range child.Fields {
				if f.Type == FieldTypeLink {
					if f.LinkTarget == "" {
						return errors.New(errors.CodeValidation, fmt.Sprintf("Link field %q in child table %q of %q lacks a link target", f.Name, child.TypeName, doc.Name), nil, nil)
					}
					if _, exists := r.docs[f.LinkTarget]; !exists {
						return errors.New(errors.CodeValidation, fmt.Sprintf("Link field %q in child table %q of %q references non-existent DocType %q", f.Name, child.TypeName, doc.Name, f.LinkTarget), nil, nil)
					}
				}
			}
		}
	}

	r.compiled = true
	return nil
}

func (r *registry) Get(docType string) (*CompiledDoc, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	doc, ok := r.docs[docType]
	if !ok {
		return nil, errors.New(errors.CodeNotFound, fmt.Sprintf("DocType %q not found in Registry", docType), nil, nil)
	}
	return doc, nil
}

func (r *registry) List() []*CompiledDoc {
	r.mu.RLock()
	defer r.mu.RUnlock()

	list := make([]*CompiledDoc, 0, len(r.docs))
	for _, doc := range r.docs {
		list = append(list, doc)
	}
	return list
}

func (r *registry) Relationships(docType string) []Relationship {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var rels []Relationship

	// Find relationships where docType is the source (from)
	if doc, ok := r.docs[docType]; ok {
		for _, f := range doc.Fields {
			if f.Type == FieldTypeLink {
				rels = append(rels, Relationship{
					FromDoc:      docType,
					FromField:    f.Name,
					ToDoc:        f.LinkTarget,
					IsChildTable: false,
				})
			}
		}
		for _, child := range doc.ChildTables {
			rels = append(rels, Relationship{
				FromDoc:      docType,
				FromField:    child.FieldName,
				ToDoc:        child.DocType,
				IsChildTable: true,
			})
			// Also inspect child table fields for links
			for _, f := range child.Fields {
				if f.Type == FieldTypeLink {
					rels = append(rels, Relationship{
						FromDoc:      child.DocType,
						FromField:    f.Name,
						ToDoc:        f.LinkTarget,
						IsChildTable: false,
					})
				}
			}
		}
	}

	return rels
}

func getBaseDocumentFields() []Field {
	return []Field{
		{Name: "ID", DBColumn: "id", Type: FieldTypeString, Required: true, Label: "ID"},
		{Name: "Name", DBColumn: "name", Type: FieldTypeString, Label: "Name"},
		{Name: "Owner", DBColumn: "owner", Type: FieldTypeString, Label: "Owner"},
		{Name: "CreatedAt", DBColumn: "created_at", Type: FieldTypeDateTime, Label: "Created At"},
		{Name: "UpdatedAt", DBColumn: "updated_at", Type: FieldTypeDateTime, Label: "Updated At"},
		{Name: "ModifiedBy", DBColumn: "modified_by", Type: FieldTypeString, Label: "Modified By"},
		{Name: "DocStatus", DBColumn: "doc_status", Type: FieldTypeInt, Label: "Doc Status"},
		{Name: "Deleted", DBColumn: "deleted", Type: FieldTypeBool, Label: "Deleted"},
	}
}

func getBaseChildFields() []Field {
	return []Field{
		{Name: "ID", DBColumn: "id", Type: FieldTypeString, Required: true, Label: "ID"},
		{Name: "ParentID", DBColumn: "parent_id", Type: FieldTypeString, Required: true, Label: "Parent ID"},
		{Name: "Idx", DBColumn: "idx", Type: FieldTypeInt, Required: true, Label: "Idx"},
	}
}
