package handlers

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/escalated-dev/escalated-go/models"
	"github.com/escalated-dev/escalated-go/services"
)

// Custom fields and custom objects admin (Laravel Admin\CustomFieldController
// and Admin\CustomObjectController): the Escalated/Admin/CustomFields/* and
// Escalated/Admin/CustomObjects/* pages and their writes at
// /admin/custom-fields and /admin/custom-objects.

func (h *AdminHandler) customFields(w http.ResponseWriter) *services.CustomFieldService {
	if h.CustomFields == nil || h.CustomFields.DB == nil {
		http.Error(w, "custom fields are not configured", http.StatusNotFound)
		return nil
	}
	return h.CustomFields
}

func jsonOrNull(j models.JSONText) any {
	if len(j) == 0 {
		return nil
	}
	var v any
	if err := json.Unmarshal(j, &v); err != nil {
		return string(j)
	}
	return v
}

// customFieldProps is a field with Laravel's column names.
func customFieldProps(f *models.CustomField) map[string]any {
	var options any
	if f.Options != nil {
		options = f.Options
	}
	return map[string]any{
		"id":               f.ID,
		"name":             f.Name,
		"slug":             f.Slug,
		"type":             f.FieldType,
		"context":          f.EntityType,
		"options":          options,
		"required":         f.IsRequired,
		"placeholder":      f.Placeholder,
		"description":      f.Description,
		"validation_rules": jsonOrNull(f.ValidationRules),
		"conditions":       jsonOrNull(f.Conditions),
		"position":         f.Position,
		"active":           f.IsActive,
		"created_at":       uiTimeValue(f.CreatedAt),
		"updated_at":       uiTimeValue(f.UpdatedAt),
	}
}

func (h *AdminHandler) customFieldsIndexURL() string { return h.adminURL("/custom-fields") }

// ListCustomFields renders Escalated/Admin/CustomFields/Index
// (GET /admin/custom-fields).
func (h *AdminHandler) ListCustomFields(w http.ResponseWriter, r *http.Request) {
	svc := h.customFields(w)
	if svc == nil {
		return
	}
	fields, err := svc.ListFields(r.Context())
	if err != nil {
		http.Error(w, "could not load the custom fields", http.StatusInternalServerError)
		return
	}
	rows := make([]map[string]any, 0, len(fields))
	for _, f := range fields {
		rows = append(rows, customFieldProps(f))
	}
	_ = h.renderer.Render(w, r, "Escalated/Admin/CustomFields/Index", map[string]any{"fields": rows})
}

func (h *AdminHandler) renderCustomFieldForm(w http.ResponseWriter, r *http.Request, f *models.CustomField, errs map[string]string) {
	props := map[string]any{"contexts": models.CustomFieldContexts}
	if f != nil {
		props["field"] = customFieldProps(f)
	}
	if errs != nil {
		props["errors"] = pageErrors(errs)
	}
	_ = h.renderer.Render(w, r, "Escalated/Admin/CustomFields/Form", props)
}

// CreateCustomFieldForm renders the form for a new field
// (GET /admin/custom-fields/create).
func (h *AdminHandler) CreateCustomFieldForm(w http.ResponseWriter, r *http.Request) {
	if h.customFields(w) == nil {
		return
	}
	h.renderCustomFieldForm(w, r, nil, nil)
}

func (h *AdminHandler) customFieldFromPath(w http.ResponseWriter, r *http.Request) (*services.CustomFieldService, *models.CustomField, bool) {
	svc := h.customFields(w)
	if svc == nil {
		return nil, nil, false
	}
	id, err := idFromPathName(r, "id")
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return nil, nil, false
	}
	f, err := svc.GetField(r.Context(), id)
	if err != nil {
		http.Error(w, "could not load the custom field", http.StatusInternalServerError)
		return nil, nil, false
	}
	if f == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return nil, nil, false
	}
	return svc, f, true
}

// EditCustomFieldForm renders the form with the field
// (GET /admin/custom-fields/{id}/edit).
func (h *AdminHandler) EditCustomFieldForm(w http.ResponseWriter, r *http.Request) {
	_, f, ok := h.customFieldFromPath(w, r)
	if !ok {
		return
	}
	h.renderCustomFieldForm(w, r, f, nil)
}

// applyCustomFieldInput validates Laravel's rules (name required|max:255, type
// and context in their lists, options array, required/active boolean,
// placeholder max:255, validation_rules array, position integer) into f.
// select and multi_select need at least one option.
func applyCustomFieldInput(in map[string]any, f *models.CustomField) map[string]string {
	errs := map[string]string{}
	str := func(key string) (string, bool) {
		v, ok := in[key]
		if !ok || v == nil {
			return "", ok
		}
		s, isString := v.(string)
		if !isString {
			errs[key] = "The " + strings.ReplaceAll(key, "_", " ") + " must be a string."
		}
		return strings.TrimSpace(s), true
	}

	name, _ := str("name")
	switch {
	case name == "":
		errs["name"] = requiredMessage("name")
	case utf8.RuneCountInString(name) > 255:
		errs["name"] = "The name may not be greater than 255 characters."
	}
	f.Name = name

	fieldType, _ := str("type")
	if fieldType == "" {
		errs["type"] = requiredMessage("type")
	} else if !slices.Contains(models.TicketFieldTypes, fieldType) {
		errs["type"] = "The selected type is invalid."
	}
	f.FieldType = fieldType

	fieldContext, _ := str("context")
	if fieldContext == "" {
		errs["context"] = requiredMessage("context")
	} else if !slices.Contains(models.CustomFieldContexts, fieldContext) {
		errs["context"] = "The selected context is invalid."
	}
	f.EntityType = fieldContext

	if raw, ok := in["options"]; ok {
		switch opts := raw.(type) {
		case nil:
			f.Options = nil
		case []any:
			f.Options = []string{}
			for _, o := range opts {
				s, isString := o.(string)
				if !isString {
					errs["options"] = "The options must be a list of strings."
					break
				}
				if s = strings.TrimSpace(s); s != "" && !slices.Contains(f.Options, s) {
					f.Options = append(f.Options, s)
				}
			}
		default:
			errs["options"] = "The options must be an array."
		}
	}
	if (fieldType == models.FieldTypeSelect || fieldType == models.FieldTypeMultiSelect) && len(f.Options) == 0 && errs["options"] == "" {
		errs["options"] = "A " + strings.ReplaceAll(fieldType, "_", "-") + " field needs at least one option."
	}

	if raw, ok := in["required"]; ok {
		b, valid := boolish(raw)
		if !valid {
			errs["required"] = "The required field must be true or false."
		}
		f.IsRequired = b
	}
	if raw, ok := in["active"]; ok {
		b, valid := boolish(raw)
		if !valid {
			errs["active"] = "The active field must be true or false."
		}
		f.IsActive = b
	}

	if v, ok := str("placeholder"); ok {
		switch {
		case v == "":
			f.Placeholder = nil
		case utf8.RuneCountInString(v) > 255:
			errs["placeholder"] = "The placeholder may not be greater than 255 characters."
		default:
			f.Placeholder = &v
		}
	}
	if v, ok := str("description"); ok {
		if v == "" {
			f.Description = nil
		} else {
			f.Description = &v
		}
	}

	if raw, ok := in["validation_rules"]; ok {
		switch raw.(type) {
		case nil:
			f.ValidationRules = nil
		case []any, map[string]any:
			b, _ := json.Marshal(raw)
			f.ValidationRules = models.JSONText(b)
		default:
			errs["validation_rules"] = "The validation rules must be an array."
		}
	}

	if raw, ok := in["position"]; ok && raw != nil {
		n, isNum := raw.(float64)
		if !isNum || n != float64(int(n)) {
			errs["position"] = "The position must be an integer."
		} else {
			f.Position = int(n)
		}
	}
	return errs
}

func decodeObject(r *http.Request) (map[string]any, bool) {
	in := map[string]any{}
	if r.Body == nil {
		return in, true
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil && !errors.Is(err, io.EOF) {
		return nil, false
	}
	if in == nil {
		in = map[string]any{}
	}
	return in, true
}

// StoreCustomField handles POST /admin/custom-fields
// (escalated.admin.custom-fields.store).
func (h *AdminHandler) StoreCustomField(w http.ResponseWriter, r *http.Request) {
	svc := h.customFields(w)
	if svc == nil {
		return
	}
	in, ok := decodeObject(r)
	if !ok {
		http.Error(w, "invalid input", http.StatusBadRequest)
		return
	}
	f := &models.CustomField{IsActive: true}
	if errs := applyCustomFieldInput(in, f); len(errs) > 0 {
		validationErrors(w, r, errs, func(errs map[string]string) { h.renderCustomFieldForm(w, r, nil, errs) })
		return
	}
	if err := svc.CreateField(r.Context(), f); err != nil {
		http.Error(w, "could not save the custom field", http.StatusInternalServerError)
		return
	}
	h.audit(r, "created", "custom_field", f.ID, nil, customFieldProps(f))
	h.pageDone(w, r, h.customFieldsIndexURL(), http.StatusCreated, map[string]any{"message": "Custom field created.", "field": customFieldProps(f)})
}

// UpdateCustomField handles PUT/PATCH /admin/custom-fields/{id}
// (escalated.admin.custom-fields.update).
func (h *AdminHandler) UpdateCustomField(w http.ResponseWriter, r *http.Request) {
	svc, f, ok := h.customFieldFromPath(w, r)
	if !ok {
		return
	}
	old := *f
	in, ok := decodeObject(r)
	if !ok {
		http.Error(w, "invalid input", http.StatusBadRequest)
		return
	}
	if errs := applyCustomFieldInput(in, f); len(errs) > 0 {
		validationErrors(w, r, errs, func(errs map[string]string) { h.renderCustomFieldForm(w, r, &old, errs) })
		return
	}
	if err := svc.UpdateField(r.Context(), f); err != nil {
		http.Error(w, "could not save the custom field", http.StatusInternalServerError)
		return
	}
	h.audit(r, "updated", "custom_field", f.ID, customFieldProps(&old), customFieldProps(f))
	h.pageDone(w, r, h.customFieldsIndexURL(), http.StatusOK, map[string]any{"message": "Custom field updated.", "field": customFieldProps(f)})
}

// DeleteCustomField handles DELETE /admin/custom-fields/{id}
// (escalated.admin.custom-fields.destroy). The field's values go with it.
func (h *AdminHandler) DeleteCustomField(w http.ResponseWriter, r *http.Request) {
	svc, f, ok := h.customFieldFromPath(w, r)
	if !ok {
		return
	}
	if err := svc.DeleteField(r.Context(), f.ID); err != nil {
		http.Error(w, "could not delete the custom field", http.StatusInternalServerError)
		return
	}
	h.audit(r, "deleted", "custom_field", f.ID, customFieldProps(f), nil)
	h.pageDone(w, r, h.customFieldsIndexURL(), http.StatusOK, map[string]any{"message": "Custom field deleted."})
}

// ReorderCustomFields handles POST /admin/custom-fields/reorder
// (escalated.admin.custom-fields.reorder): positions [{id, position}].
func (h *AdminHandler) ReorderCustomFields(w http.ResponseWriter, r *http.Request) {
	svc := h.customFields(w)
	if svc == nil {
		return
	}
	in, ok := decodeObject(r)
	if !ok {
		http.Error(w, "invalid input", http.StatusBadRequest)
		return
	}
	items, isList := in["positions"].([]any)
	errs := map[string]string{}
	positions := map[int64]int{}
	var ids []int64
	if !isList || len(items) == 0 {
		errs["positions"] = requiredMessage("positions")
	}
	for i, item := range items {
		m, _ := item.(map[string]any)
		id, idOK := m["id"].(float64)
		pos, posOK := m["position"].(float64)
		if !idOK || id != float64(int64(id)) {
			errs["positions."+strconv.Itoa(i)+".id"] = "The id must be an integer."
			continue
		}
		if !posOK || pos != float64(int(pos)) {
			errs["positions."+strconv.Itoa(i)+".position"] = "The position must be an integer."
			continue
		}
		positions[int64(id)] = int(pos)
		ids = append(ids, int64(id))
	}
	if len(errs) == 0 {
		exist, err := svc.FieldsExist(r.Context(), ids)
		if err != nil {
			http.Error(w, "could not reorder the custom fields", http.StatusInternalServerError)
			return
		}
		if !exist {
			errs["positions"] = "The selected custom field is invalid."
		}
	}
	if len(errs) > 0 {
		validationErrors(w, r, errs, nil)
		return
	}
	if err := svc.ReorderFields(r.Context(), positions); err != nil {
		http.Error(w, "could not reorder the custom fields", http.StatusInternalServerError)
		return
	}
	if isInertia(r) {
		backTo(w, r, h.customFieldsIndexURL())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"message": "Fields reordered."})
}

// --- Custom objects ---

func customObjectProps(o *models.CustomObject, schema []services.ObjectField) map[string]any {
	return map[string]any{
		"id":            o.ID,
		"name":          o.Name,
		"slug":          o.Slug,
		"fields_schema": schema,
		"created_at":    uiTimeValue(o.CreatedAt),
		"updated_at":    uiTimeValue(o.UpdatedAt),
	}
}

func customRecordProps(rec *models.CustomObjectRecord) map[string]any {
	data := jsonOrNull(rec.Data)
	if data == nil {
		data = map[string]any{}
	}
	return map[string]any{
		"id":         rec.ID,
		"object_id":  rec.CustomObjectID,
		"data":       data,
		"created_at": uiTimeValue(rec.CreatedAt),
		"updated_at": uiTimeValue(rec.UpdatedAt),
	}
}

func (h *AdminHandler) customObjectsIndexURL() string { return h.adminURL("/custom-objects") }

// ListCustomObjects renders Escalated/Admin/CustomObjects/Index
// (GET /admin/custom-objects).
func (h *AdminHandler) ListCustomObjects(w http.ResponseWriter, r *http.Request) {
	svc := h.customFields(w)
	if svc == nil {
		return
	}
	objects, err := svc.ListObjects(r.Context())
	if err != nil {
		http.Error(w, "could not load the custom objects", http.StatusInternalServerError)
		return
	}
	rows := make([]map[string]any, 0, len(objects))
	for _, o := range objects {
		row := customObjectProps(o.Object, o.Schema)
		row["records_count"] = o.RecordsCount
		rows = append(rows, row)
	}
	_ = h.renderer.Render(w, r, "Escalated/Admin/CustomObjects/Index", map[string]any{"objects": rows})
}

func (h *AdminHandler) renderCustomObjectForm(w http.ResponseWriter, r *http.Request, o *models.CustomObject, schema []services.ObjectField, errs map[string]string) {
	props := map[string]any{"object": nil}
	if o != nil {
		props["object"] = customObjectProps(o, schema)
	}
	if errs != nil {
		props["errors"] = pageErrors(errs)
	}
	_ = h.renderer.Render(w, r, "Escalated/Admin/CustomObjects/Form", props)
}

// CreateCustomObjectForm renders the form for a new object
// (GET /admin/custom-objects/create).
func (h *AdminHandler) CreateCustomObjectForm(w http.ResponseWriter, r *http.Request) {
	if h.customFields(w) == nil {
		return
	}
	h.renderCustomObjectForm(w, r, nil, nil, nil)
}

func (h *AdminHandler) customObjectFromPath(w http.ResponseWriter, r *http.Request) (*services.CustomFieldService, *models.CustomObject, []services.ObjectField, bool) {
	svc := h.customFields(w)
	if svc == nil {
		return nil, nil, nil, false
	}
	id, err := idFromPathName(r, "id")
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return nil, nil, nil, false
	}
	o, schema, err := svc.GetObject(r.Context(), id)
	if err != nil {
		http.Error(w, "could not load the custom object", http.StatusInternalServerError)
		return nil, nil, nil, false
	}
	if o == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return nil, nil, nil, false
	}
	return svc, o, schema, true
}

// EditCustomObjectForm renders the form with the object
// (GET /admin/custom-objects/{id}/edit).
func (h *AdminHandler) EditCustomObjectForm(w http.ResponseWriter, r *http.Request) {
	_, o, schema, ok := h.customObjectFromPath(w, r)
	if !ok {
		return
	}
	h.renderCustomObjectForm(w, r, o, schema, nil)
}

var alphaDash = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// applyCustomObjectInput validates Laravel's rules: name required|max:255,
// slug required|max:100|alpha_dash|unique, fields_schema required|array|min:1
// with each name required|max:100, type in the object field types, required
// boolean and options array.
func (h *AdminHandler) applyCustomObjectInput(r *http.Request, svc *services.CustomFieldService, in map[string]any, o *models.CustomObject) ([]services.ObjectField, map[string]string, error) {
	errs := map[string]string{}
	name, _ := in["name"].(string)
	name = strings.TrimSpace(name)
	switch {
	case name == "":
		errs["name"] = requiredMessage("name")
	case utf8.RuneCountInString(name) > 255:
		errs["name"] = "The name may not be greater than 255 characters."
	}
	o.Name = name

	slug, _ := in["slug"].(string)
	slug = strings.TrimSpace(slug)
	switch {
	case slug == "":
		errs["slug"] = requiredMessage("slug")
	case len(slug) > 100:
		errs["slug"] = "The slug may not be greater than 100 characters."
	case !alphaDash.MatchString(slug):
		errs["slug"] = "The slug may only contain letters, numbers, dashes and underscores."
	default:
		taken, err := svc.ObjectSlugTaken(r.Context(), slug, o.ID)
		if err != nil {
			return nil, nil, err
		}
		if taken {
			errs["slug"] = "The slug has already been taken."
		}
	}
	o.Slug = slug

	items, isList := in["fields_schema"].([]any)
	if !isList || len(items) == 0 {
		errs["fields_schema"] = "The fields schema must have at least 1 field."
	}
	schema := make([]services.ObjectField, 0, len(items))
	for i, item := range items {
		key := "fields_schema." + strconv.Itoa(i) + "."
		m, _ := item.(map[string]any)
		field := services.ObjectField{}
		fname, _ := m["name"].(string)
		field.Name = strings.TrimSpace(fname)
		switch {
		case field.Name == "":
			errs[key+"name"] = requiredMessage("field name")
		case utf8.RuneCountInString(field.Name) > 100:
			errs[key+"name"] = "The field name may not be greater than 100 characters."
		}
		field.Type, _ = m["type"].(string)
		if !slices.Contains(services.ObjectFieldTypes, field.Type) {
			errs[key+"type"] = "The selected field type is invalid."
		}
		if raw, ok := m["required"]; ok {
			b, valid := boolish(raw)
			if !valid {
				errs[key+"required"] = "The required field must be true or false."
			}
			field.Required = b
		}
		if raw, ok := m["options"]; ok && raw != nil {
			opts, isArr := raw.([]any)
			if !isArr {
				errs[key+"options"] = "The options must be an array."
			}
			for _, opt := range opts {
				if s, isString := opt.(string); isString && strings.TrimSpace(s) != "" {
					field.Options = append(field.Options, strings.TrimSpace(s))
				}
			}
		}
		schema = append(schema, field)
	}
	return schema, errs, nil
}

func (h *AdminHandler) saveCustomObject(w http.ResponseWriter, r *http.Request, svc *services.CustomFieldService, o *models.CustomObject, old *models.CustomObject, oldSchema []services.ObjectField) {
	in, ok := decodeObject(r)
	if !ok {
		http.Error(w, "invalid input", http.StatusBadRequest)
		return
	}
	creating := o.ID == 0
	schema, errs, err := h.applyCustomObjectInput(r, svc, in, o)
	if err != nil {
		http.Error(w, "could not save the custom object", http.StatusInternalServerError)
		return
	}
	if len(errs) > 0 {
		validationErrors(w, r, errs, func(errs map[string]string) { h.renderCustomObjectForm(w, r, old, oldSchema, errs) })
		return
	}
	if err := svc.SaveObject(r.Context(), o, schema); err != nil {
		http.Error(w, "could not save the custom object", http.StatusInternalServerError)
		return
	}
	if creating {
		h.audit(r, "created", "custom_object", o.ID, nil, customObjectProps(o, schema))
		h.pageDone(w, r, h.customObjectsIndexURL(), http.StatusCreated, map[string]any{"message": "Custom object created.", "object": customObjectProps(o, schema)})
		return
	}
	h.audit(r, "updated", "custom_object", o.ID, customObjectProps(old, oldSchema), customObjectProps(o, schema))
	h.pageDone(w, r, h.customObjectsIndexURL(), http.StatusOK, map[string]any{"message": "Custom object updated.", "object": customObjectProps(o, schema)})
}

// StoreCustomObject handles POST /admin/custom-objects
// (escalated.admin.custom-objects.store).
func (h *AdminHandler) StoreCustomObject(w http.ResponseWriter, r *http.Request) {
	svc := h.customFields(w)
	if svc == nil {
		return
	}
	h.saveCustomObject(w, r, svc, &models.CustomObject{}, nil, nil)
}

// UpdateCustomObject handles PUT/PATCH /admin/custom-objects/{id}
// (escalated.admin.custom-objects.update).
func (h *AdminHandler) UpdateCustomObject(w http.ResponseWriter, r *http.Request) {
	svc, o, schema, ok := h.customObjectFromPath(w, r)
	if !ok {
		return
	}
	old := *o
	h.saveCustomObject(w, r, svc, o, &old, schema)
}

// DeleteCustomObject handles DELETE /admin/custom-objects/{id}
// (escalated.admin.custom-objects.destroy). Its records go with it.
func (h *AdminHandler) DeleteCustomObject(w http.ResponseWriter, r *http.Request) {
	svc, o, schema, ok := h.customObjectFromPath(w, r)
	if !ok {
		return
	}
	if err := svc.DeleteObject(r.Context(), o.ID); err != nil {
		http.Error(w, "could not delete the custom object", http.StatusInternalServerError)
		return
	}
	h.audit(r, "deleted", "custom_object", o.ID, customObjectProps(o, schema), nil)
	h.pageDone(w, r, h.customObjectsIndexURL(), http.StatusOK, map[string]any{"message": "Custom object deleted."})
}

func (h *AdminHandler) renderCustomObjectRecords(w http.ResponseWriter, r *http.Request, svc *services.CustomFieldService, o *models.CustomObject, schema []services.ObjectField, errs map[string]string) {
	records, err := svc.ListRecords(r.Context(), o.ID)
	if err != nil {
		http.Error(w, "could not load the records", http.StatusInternalServerError)
		return
	}
	rows := make([]map[string]any, 0, len(records))
	for _, rec := range records {
		rows = append(rows, customRecordProps(rec))
	}
	props := map[string]any{"object": customObjectProps(o, schema), "records": rows}
	if errs != nil {
		props["errors"] = pageErrors(errs)
	}
	_ = h.renderer.Render(w, r, "Escalated/Admin/CustomObjects/Records", props)
}

// CustomObjectRecords renders Escalated/Admin/CustomObjects/Records
// (GET /admin/custom-objects/{id}/records), newest record first.
func (h *AdminHandler) CustomObjectRecords(w http.ResponseWriter, r *http.Request) {
	svc, o, schema, ok := h.customObjectFromPath(w, r)
	if !ok {
		return
	}
	h.renderCustomObjectRecords(w, r, svc, o, schema, nil)
}

func (h *AdminHandler) recordsURL(o *models.CustomObject) string {
	return h.adminURL("/custom-objects/" + strconv.FormatInt(o.ID, 10) + "/records")
}

// recordData reads the record's data (required, an object) and validates it
// against the schema.
func recordData(r *http.Request, schema []services.ObjectField) (map[string]any, map[string]string, bool) {
	in, ok := decodeObject(r)
	if !ok {
		return nil, nil, false
	}
	data, isObject := in["data"].(map[string]any)
	if !isObject {
		return nil, map[string]string{"data": requiredMessage("data")}, true
	}
	return data, services.ValidateRecordData(schema, data), true
}

func (h *AdminHandler) writeRecord(w http.ResponseWriter, r *http.Request, rec *models.CustomObjectRecord, message string) {
	svc, o, schema, ok := h.customObjectFromPath(w, r)
	if !ok {
		return
	}
	creating := rec == nil
	if !creating {
		id, err := idFromPathName(r, "record")
		if err != nil {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		rec, err = svc.GetRecord(r.Context(), o.ID, id)
		if err != nil {
			http.Error(w, "could not load the record", http.StatusInternalServerError)
			return
		}
		if rec == nil {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
	} else {
		rec = &models.CustomObjectRecord{CustomObjectID: o.ID}
	}
	data, errs, ok := recordData(r, schema)
	if !ok {
		http.Error(w, "invalid input", http.StatusBadRequest)
		return
	}
	if len(errs) > 0 {
		validationErrors(w, r, errs, func(errs map[string]string) { h.renderCustomObjectRecords(w, r, svc, o, schema, errs) })
		return
	}
	if err := svc.SaveRecord(r.Context(), rec, data); err != nil {
		http.Error(w, "could not save the record", http.StatusInternalServerError)
		return
	}
	if isInertia(r) {
		backTo(w, r, h.recordsURL(o))
		return
	}
	code := http.StatusOK
	if creating {
		code = http.StatusCreated
	}
	writeJSON(w, code, map[string]any{"message": message, "record": customRecordProps(rec)})
}

// StoreCustomObjectRecord handles POST /admin/custom-objects/{id}/records
// (escalated.admin.custom-objects.records.store).
func (h *AdminHandler) StoreCustomObjectRecord(w http.ResponseWriter, r *http.Request) {
	h.writeRecord(w, r, nil, "Record created.")
}

// UpdateCustomObjectRecord handles PUT /admin/custom-objects/{id}/records/{record}
// (escalated.admin.custom-objects.records.update). A record of another
// object is a 404.
func (h *AdminHandler) UpdateCustomObjectRecord(w http.ResponseWriter, r *http.Request) {
	h.writeRecord(w, r, &models.CustomObjectRecord{}, "Record updated.")
}

// DeleteCustomObjectRecord handles DELETE /admin/custom-objects/{id}/records/{record}
// (escalated.admin.custom-objects.records.destroy).
func (h *AdminHandler) DeleteCustomObjectRecord(w http.ResponseWriter, r *http.Request) {
	svc, o, _, ok := h.customObjectFromPath(w, r)
	if !ok {
		return
	}
	id, err := idFromPathName(r, "record")
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	rec, err := svc.GetRecord(r.Context(), o.ID, id)
	if err != nil {
		http.Error(w, "could not load the record", http.StatusInternalServerError)
		return
	}
	if rec == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if err := svc.DeleteRecord(r.Context(), o.ID, rec.ID); err != nil {
		http.Error(w, "could not delete the record", http.StatusInternalServerError)
		return
	}
	if isInertia(r) {
		backTo(w, r, h.recordsURL(o))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"message": "Record deleted."})
}

// --- Ticket values ---

// ticketCustomFieldsInput reads a ticket write's custom_fields: a nested
// object in a JSON body (readInput re-encodes it as one field), or
// custom_fields[slug] / custom_fields[slug][] form fields. present reports
// whether the request sent any; ok is false when it is not an object.
func ticketCustomFieldsInput(in url.Values) (values map[string]any, present, ok bool) {
	values = map[string]any{}
	if in.Has("custom_fields") {
		present = true
		raw := strings.TrimSpace(in.Get("custom_fields"))
		if raw != "" && raw != "null" {
			if err := json.Unmarshal([]byte(raw), &values); err != nil || values == nil {
				return nil, true, false
			}
		}
	}
	for key, vals := range in {
		rest, found := strings.CutPrefix(key, "custom_fields[")
		if !found {
			continue
		}
		present = true
		if slug, isList := strings.CutSuffix(rest, "][]"); isList {
			items := make([]any, 0, len(vals))
			for _, v := range vals {
				items = append(items, v)
			}
			values[slug] = items
			continue
		}
		slug := strings.TrimSuffix(rest, "]")
		if len(vals) > 0 {
			values[slug] = vals[0]
		} else {
			values[slug] = ""
		}
	}
	return values, present, true
}

// validateTicketCustomFields validates a ticket write's custom_fields into
// values to store, merging its errors into errs.
func validateTicketCustomFields(r *http.Request, svc *services.CustomFieldService, input map[string]any, creating bool, errs map[string]string) (services.FieldValues, error) {
	if svc == nil && len(input) == 0 {
		return nil, nil
	}
	values, fieldErrs, err := svc.ValidateTicketValues(r.Context(), input, creating)
	if err != nil {
		return nil, err
	}
	for k, v := range fieldErrs {
		errs[k] = v
	}
	return values, nil
}
