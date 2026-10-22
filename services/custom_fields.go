package services

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/escalated-dev/escalated-go/internal/sqldialect"
	"github.com/escalated-dev/escalated-go/models"
)

// CustomFieldService keeps custom field definitions, the values tickets hold
// for them, and custom objects with their records (Laravel CustomField,
// CustomFieldValue, CustomObject, CustomObjectRecord).
//
// Every table name is built from Prefix, so a custom Config.TablePrefix works
// without the ticket-access query rewriting.
type CustomFieldService struct {
	DB     *sql.DB
	Prefix string
}

// TicketEntity is the entity_type ticket values are stored under.
const TicketEntity = "ticket"

func (s *CustomFieldService) t(name string) string { return s.Prefix + name }

func (s *CustomFieldService) q(query string) string { return sqldialect.Rebind(s.DB, query) }

const customFieldColumns = "id, name, slug, field_type, description, is_required, options, default_value, placeholder, validation_rules, conditions, entity_type, position, is_active, created_at, updated_at"

func scanCustomField(row interface{ Scan(...any) error }) (*models.CustomField, error) {
	f := &models.CustomField{}
	var options sql.NullString
	if err := row.Scan(&f.ID, &f.Name, &f.Slug, &f.FieldType, &f.Description, &f.IsRequired, &options,
		&f.DefaultValue, &f.Placeholder, &f.ValidationRules, &f.Conditions, &f.EntityType, &f.Position,
		&f.IsActive, &f.CreatedAt, &f.UpdatedAt); err != nil {
		return nil, err
	}
	if options.Valid && options.String != "" {
		_ = json.Unmarshal([]byte(options.String), &f.Options)
	}
	return f, nil
}

func optionsValue(opts []string) any {
	if opts == nil {
		return nil
	}
	b, _ := json.Marshal(opts)
	return string(b)
}

// ListFields returns every field definition ordered by position, then id.
func (s *CustomFieldService) ListFields(ctx context.Context) ([]*models.CustomField, error) {
	return s.listFields(ctx, "")
}

// ActiveTicketFields returns the active ticket-context fields by position.
func (s *CustomFieldService) ActiveTicketFields(ctx context.Context) ([]*models.CustomField, error) {
	return s.listFields(ctx, "WHERE entity_type = ? AND is_active = ?", TicketEntity, true)
}

func (s *CustomFieldService) listFields(ctx context.Context, where string, args ...any) ([]*models.CustomField, error) {
	rows, err := s.DB.QueryContext(ctx, s.q("SELECT "+customFieldColumns+" FROM "+s.t("custom_fields")+" "+where+" ORDER BY position, id"), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*models.CustomField{}
	for rows.Next() {
		f, err := scanCustomField(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// GetField returns the field, or nil when there is none.
func (s *CustomFieldService) GetField(ctx context.Context, id int64) (*models.CustomField, error) {
	f, err := scanCustomField(s.DB.QueryRowContext(ctx, s.q("SELECT "+customFieldColumns+" FROM "+s.t("custom_fields")+" WHERE id = ?"), id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return f, err
}

var slugStrip = regexp.MustCompile(`[^a-z0-9]+`)

// FieldSlug is Laravel's Str::slug(name, '_') for ASCII names.
func FieldSlug(name string) string {
	return strings.Trim(slugStrip.ReplaceAllString(strings.ToLower(name), "_"), "_")
}

// uniqueSlug de-duplicates base against the table's slugs: base, base_2,
// base_3...
func (s *CustomFieldService) uniqueSlug(ctx context.Context, table, base string, exceptID int64) (string, error) {
	if base == "" {
		base = "field"
	}
	for n := 1; ; n++ {
		candidate := base
		if n > 1 {
			candidate = fmt.Sprintf("%s_%d", base, n)
		}
		var count int
		if err := s.DB.QueryRowContext(ctx, s.q("SELECT COUNT(1) FROM "+s.t(table)+" WHERE slug = ? AND id <> ?"), candidate, exceptID).Scan(&count); err != nil {
			return "", err
		}
		if count == 0 {
			return candidate, nil
		}
	}
}

// CreateField inserts f. A blank slug is generated from the name and made
// unique.
func (s *CustomFieldService) CreateField(ctx context.Context, f *models.CustomField) error {
	base := FieldSlug(f.Slug)
	if base == "" {
		base = FieldSlug(f.Name)
	}
	slug, err := s.uniqueSlug(ctx, "custom_fields", base, 0)
	if err != nil {
		return err
	}
	f.Slug = slug
	if f.EntityType == "" {
		f.EntityType = TicketEntity
	}
	now := time.Now().UTC()
	f.CreatedAt, f.UpdatedAt = now, now
	return s.DB.QueryRowContext(ctx, s.q("INSERT INTO "+s.t("custom_fields")+
		" (name, slug, field_type, description, is_required, options, default_value, placeholder, validation_rules, conditions, entity_type, position, is_active, created_at, updated_at)"+
		" VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) RETURNING id"),
		f.Name, f.Slug, f.FieldType, f.Description, f.IsRequired, optionsValue(f.Options), f.DefaultValue, f.Placeholder,
		f.ValidationRules, f.Conditions, f.EntityType, f.Position, f.IsActive, now, now).Scan(&f.ID)
}

// UpdateField saves f's editable columns. The slug never changes.
func (s *CustomFieldService) UpdateField(ctx context.Context, f *models.CustomField) error {
	f.UpdatedAt = time.Now().UTC()
	_, err := s.DB.ExecContext(ctx, s.q("UPDATE "+s.t("custom_fields")+
		" SET name = ?, field_type = ?, description = ?, is_required = ?, options = ?, placeholder = ?, validation_rules = ?, entity_type = ?, position = ?, is_active = ?, updated_at = ? WHERE id = ?"),
		f.Name, f.FieldType, f.Description, f.IsRequired, optionsValue(f.Options), f.Placeholder, f.ValidationRules,
		f.EntityType, f.Position, f.IsActive, f.UpdatedAt, f.ID)
	return err
}

// DeleteField deletes the field and every value stored for it.
func (s *CustomFieldService) DeleteField(ctx context.Context, id int64) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	// The foreign key cascades too, but SQLite only enforces it when the host
	// turned foreign_keys on.
	if _, err := tx.ExecContext(ctx, s.q("DELETE FROM "+s.t("custom_field_values")+" WHERE custom_field_id = ?"), id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, s.q("DELETE FROM "+s.t("custom_fields")+" WHERE id = ?"), id); err != nil {
		return err
	}
	return tx.Commit()
}

// ReorderFields sets each field's position.
func (s *CustomFieldService) ReorderFields(ctx context.Context, positions map[int64]int) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	now := time.Now().UTC()
	for id, pos := range positions {
		if _, err := tx.ExecContext(ctx, s.q("UPDATE "+s.t("custom_fields")+" SET position = ?, updated_at = ? WHERE id = ?"), pos, now, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// FieldsExist reports whether every id names a field.
func (s *CustomFieldService) FieldsExist(ctx context.Context, ids []int64) (bool, error) {
	for _, id := range ids {
		f, err := s.GetField(ctx, id)
		if err != nil {
			return false, err
		}
		if f == nil {
			return false, nil
		}
	}
	return true, nil
}

// --- Ticket values ---

// FieldValues is a validated custom_fields input: the encoded value to store
// per field id, nil meaning delete the stored value.
type FieldValues map[int64]*string

// ErrorKey is the validation error key for a field slug.
func ErrorKey(slug string) string { return "custom_fields." + slug }

func emptyValue(v any) bool {
	switch val := v.(type) {
	case nil:
		return true
	case string:
		return strings.TrimSpace(val) == ""
	case []any:
		return len(val) == 0
	case []string:
		return len(val) == 0
	}
	return false
}

// ValidateTicketValues checks a ticket's custom_fields input (keyed by field
// slug) against the active ticket fields. On create every required field must
// be present and non-empty; on update only the keys sent are checked. Errors
// are keyed custom_fields.<slug>. With no fields defined and no input, it
// returns nothing to store.
func (s *CustomFieldService) ValidateTicketValues(ctx context.Context, input map[string]any, creating bool) (FieldValues, map[string]string, error) {
	errs := map[string]string{}
	if s == nil || s.DB == nil {
		for key := range input {
			errs[ErrorKey(key)] = "The selected custom field is invalid."
		}
		return nil, errs, nil
	}
	if !creating && len(input) == 0 {
		return nil, errs, nil
	}
	fields, err := s.ActiveTicketFields(ctx)
	if err != nil {
		return nil, nil, err
	}
	bySlug := map[string]*models.CustomField{}
	for _, f := range fields {
		bySlug[f.Slug] = f
	}
	for key := range input {
		if bySlug[key] == nil {
			errs[ErrorKey(key)] = "The selected custom field is invalid."
		}
	}
	out := FieldValues{}
	for _, f := range fields {
		raw, present := input[f.Slug]
		if !present {
			if creating && f.IsRequired {
				errs[ErrorKey(f.Slug)] = fmt.Sprintf("The %s field is required.", f.Name)
			}
			continue
		}
		if emptyValue(raw) {
			if f.IsRequired {
				errs[ErrorKey(f.Slug)] = fmt.Sprintf("The %s field is required.", f.Name)
				continue
			}
			out[f.ID] = nil
			continue
		}
		encoded, msg := encodeFieldValue(f, raw)
		if msg != "" {
			errs[ErrorKey(f.Slug)] = msg
			continue
		}
		out[f.ID] = &encoded
	}
	return out, errs, nil
}

// encodeFieldValue validates one non-empty value and returns its stored form.
func encodeFieldValue(f *models.CustomField, raw any) (string, string) {
	str, isString := raw.(string)
	if isString {
		str = strings.TrimSpace(str)
	}
	switch f.FieldType {
	case models.FieldTypeNumber:
		switch v := raw.(type) {
		case float64:
			if math.IsNaN(v) || math.IsInf(v, 0) {
				break
			}
			return strconv.FormatFloat(v, 'f', -1, 64), ""
		case string:
			if n, err := strconv.ParseFloat(str, 64); err == nil && !math.IsNaN(n) && !math.IsInf(n, 0) {
				return str, ""
			}
		}
		return "", fmt.Sprintf("The %s field must be a number.", f.Name)
	case models.FieldTypeDate:
		if isString {
			if _, err := time.Parse("2006-01-02", str); err == nil {
				return str, ""
			}
		}
		return "", fmt.Sprintf("The %s field must be a valid date (YYYY-MM-DD).", f.Name)
	case models.FieldTypeCheckbox:
		switch v := raw.(type) {
		case bool:
			if v {
				return "1", ""
			}
			return "0", ""
		case float64:
			if v == 1 {
				return "1", ""
			}
			if v == 0 {
				return "0", ""
			}
		case string:
			switch strings.ToLower(str) {
			case "1", "true":
				return "1", ""
			case "0", "false":
				return "0", ""
			}
		}
		return "", fmt.Sprintf("The %s field must be true or false.", f.Name)
	case models.FieldTypeSelect:
		if isString && slices.Contains(f.Options, str) {
			return str, ""
		}
		return "", fmt.Sprintf("The selected %s is invalid.", f.Name)
	case models.FieldTypeMultiSelect:
		var items []string
		switch v := raw.(type) {
		case []any:
			for _, item := range v {
				s, ok := item.(string)
				if !ok {
					return "", fmt.Sprintf("The selected %s is invalid.", f.Name)
				}
				items = append(items, s)
			}
		case []string:
			items = v
		default:
			return "", fmt.Sprintf("The %s field must be a list.", f.Name)
		}
		for _, item := range items {
			if !slices.Contains(f.Options, item) {
				return "", fmt.Sprintf("The selected %s is invalid.", f.Name)
			}
		}
		b, _ := json.Marshal(items)
		return string(b), ""
	case models.FieldTypeText:
		if !isString {
			return "", fmt.Sprintf("The %s field must be a string.", f.Name)
		}
		if utf8.RuneCountInString(str) > 255 {
			return "", fmt.Sprintf("The %s field may not be greater than 255 characters.", f.Name)
		}
		return str, ""
	default: // textarea, url and anything older
		if !isString {
			return "", fmt.Sprintf("The %s field must be a string.", f.Name)
		}
		return str, ""
	}
}

// SaveTicketValues stores (or, for nil, deletes) the ticket's values.
func (s *CustomFieldService) SaveTicketValues(ctx context.Context, ticketID int64, values FieldValues) error {
	if s == nil || len(values) == 0 {
		return nil
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	now := time.Now().UTC()
	table := s.t("custom_field_values")
	for fieldID, value := range values {
		if _, err := tx.ExecContext(ctx, s.q("DELETE FROM "+table+" WHERE custom_field_id = ? AND entity_type = ? AND entity_id = ?"), fieldID, TicketEntity, ticketID); err != nil {
			return err
		}
		if value == nil {
			continue
		}
		if _, err := tx.ExecContext(ctx, s.q("INSERT INTO "+table+" (custom_field_id, entity_type, entity_id, value, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)"),
			fieldID, TicketEntity, ticketID, *value, now, now); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// FieldDefinition is a field as the ticket pages read it.
func FieldDefinition(f *models.CustomField) map[string]any {
	options := f.Options
	if options == nil {
		options = []string{}
	}
	return map[string]any{
		"id":          f.ID,
		"name":        f.Name,
		"slug":        f.Slug,
		"type":        f.FieldType,
		"options":     options,
		"required":    f.IsRequired,
		"placeholder": f.Placeholder,
		"description": f.Description,
		"position":    f.Position,
	}
}

// TicketFieldDefinitions lists the active ticket fields for the create pages.
func (s *CustomFieldService) TicketFieldDefinitions(ctx context.Context) []map[string]any {
	out := []map[string]any{}
	if s == nil || s.DB == nil {
		return out
	}
	fields, err := s.ActiveTicketFields(ctx)
	if err != nil {
		return out
	}
	for _, f := range fields {
		out = append(out, FieldDefinition(f))
	}
	return out
}

// TicketFieldValues lists the active ticket fields with the ticket's decoded
// value (null when unset) for the show pages and the JSON detail.
func (s *CustomFieldService) TicketFieldValues(ctx context.Context, ticketID int64) []map[string]any {
	out := []map[string]any{}
	if s == nil || s.DB == nil {
		return out
	}
	fields, err := s.ActiveTicketFields(ctx)
	if err != nil || len(fields) == 0 {
		return out
	}
	stored := map[int64]string{}
	rows, err := s.DB.QueryContext(ctx, s.q("SELECT custom_field_id, value FROM "+s.t("custom_field_values")+" WHERE entity_type = ? AND entity_id = ?"), TicketEntity, ticketID)
	if err == nil {
		for rows.Next() {
			var id int64
			var v sql.NullString
			if rows.Scan(&id, &v) == nil && v.Valid {
				stored[id] = v.String
			}
		}
		rows.Close()
	}
	for _, f := range fields {
		def := FieldDefinition(f)
		def["value"] = nil
		if v, ok := stored[f.ID]; ok {
			def["value"] = decodeFieldValue(f.FieldType, v)
		}
		out = append(out, def)
	}
	return out
}

func decodeFieldValue(fieldType, v string) any {
	switch fieldType {
	case models.FieldTypeMultiSelect:
		items := []string{}
		_ = json.Unmarshal([]byte(v), &items)
		return items
	case models.FieldTypeCheckbox:
		return v == "1" || v == "true"
	case models.FieldTypeNumber:
		if n, err := strconv.ParseFloat(v, 64); err == nil {
			return n
		}
	}
	return v
}

// --- Custom objects ---

// ObjectField is one entry of a custom object's fields_schema.
type ObjectField struct {
	Name     string   `json:"name"`
	Type     string   `json:"type"`
	Required bool     `json:"required"`
	Options  []string `json:"options,omitempty"`
}

// ObjectFieldTypes are the types a custom object field may have.
var ObjectFieldTypes = []string{"text", "number", "select", "date", "lookup"}

// CustomObjectRow is a custom object with its record count.
type CustomObjectRow struct {
	Object       *models.CustomObject
	Schema       []ObjectField
	RecordsCount int
}

func schemaOf(o *models.CustomObject) []ObjectField {
	schema := []ObjectField{}
	if len(o.FieldDefinitions) > 0 {
		_ = json.Unmarshal(o.FieldDefinitions, &schema)
	}
	if schema == nil {
		schema = []ObjectField{}
	}
	return schema
}

const customObjectColumns = "id, name, slug, description, field_definitions, is_active, created_at, updated_at"

func scanObject(row interface{ Scan(...any) error }) (*models.CustomObject, error) {
	o := &models.CustomObject{}
	err := row.Scan(&o.ID, &o.Name, &o.Slug, &o.Description, &o.FieldDefinitions, &o.IsActive, &o.CreatedAt, &o.UpdatedAt)
	return o, err
}

// ListObjects returns every object by name with its schema and record count.
func (s *CustomFieldService) ListObjects(ctx context.Context) ([]CustomObjectRow, error) {
	rows, err := s.DB.QueryContext(ctx, s.q("SELECT "+customObjectColumns+" FROM "+s.t("custom_objects")+" ORDER BY name, id"))
	if err != nil {
		return nil, err
	}
	var objects []*models.CustomObject
	for rows.Next() {
		o, err := scanObject(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		objects = append(objects, o)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	counts := map[int64]int{}
	crows, err := s.DB.QueryContext(ctx, "SELECT custom_object_id, COUNT(1) FROM "+s.t("custom_object_records")+" GROUP BY custom_object_id")
	if err != nil {
		return nil, err
	}
	for crows.Next() {
		var id int64
		var n int
		if err := crows.Scan(&id, &n); err != nil {
			crows.Close()
			return nil, err
		}
		counts[id] = n
	}
	crows.Close()
	out := make([]CustomObjectRow, 0, len(objects))
	for _, o := range objects {
		out = append(out, CustomObjectRow{Object: o, Schema: schemaOf(o), RecordsCount: counts[o.ID]})
	}
	return out, nil
}

// GetObject returns the object and its schema, or nil.
func (s *CustomFieldService) GetObject(ctx context.Context, id int64) (*models.CustomObject, []ObjectField, error) {
	o, err := scanObject(s.DB.QueryRowContext(ctx, s.q("SELECT "+customObjectColumns+" FROM "+s.t("custom_objects")+" WHERE id = ?"), id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	return o, schemaOf(o), nil
}

// ObjectSlugTaken reports whether another object already has the slug.
func (s *CustomFieldService) ObjectSlugTaken(ctx context.Context, slug string, exceptID int64) (bool, error) {
	var n int
	err := s.DB.QueryRowContext(ctx, s.q("SELECT COUNT(1) FROM "+s.t("custom_objects")+" WHERE slug = ? AND id <> ?"), slug, exceptID).Scan(&n)
	return n > 0, err
}

// SaveObject inserts (ID 0) or updates the object with the schema.
func (s *CustomFieldService) SaveObject(ctx context.Context, o *models.CustomObject, schema []ObjectField) error {
	b, err := json.Marshal(schema)
	if err != nil {
		return err
	}
	o.FieldDefinitions = models.JSONText(b)
	now := time.Now().UTC()
	o.UpdatedAt = now
	if o.ID == 0 {
		o.CreatedAt = now
		o.IsActive = true
		return s.DB.QueryRowContext(ctx, s.q("INSERT INTO "+s.t("custom_objects")+" (name, slug, field_definitions, is_active, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?) RETURNING id"),
			o.Name, o.Slug, string(b), true, now, now).Scan(&o.ID)
	}
	_, err = s.DB.ExecContext(ctx, s.q("UPDATE "+s.t("custom_objects")+" SET name = ?, slug = ?, field_definitions = ?, updated_at = ? WHERE id = ?"),
		o.Name, o.Slug, string(b), now, o.ID)
	return err
}

// DeleteObject deletes the object and its records.
func (s *CustomFieldService) DeleteObject(ctx context.Context, id int64) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, s.q("DELETE FROM "+s.t("custom_object_records")+" WHERE custom_object_id = ?"), id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, s.q("DELETE FROM "+s.t("custom_objects")+" WHERE id = ?"), id); err != nil {
		return err
	}
	return tx.Commit()
}

const recordColumns = "id, custom_object_id, title, data, linked_entity_type, linked_entity_id, created_at, updated_at"

func scanRecord(row interface{ Scan(...any) error }) (*models.CustomObjectRecord, error) {
	r := &models.CustomObjectRecord{}
	err := row.Scan(&r.ID, &r.CustomObjectID, &r.Title, &r.Data, &r.LinkedEntityType, &r.LinkedEntityID, &r.CreatedAt, &r.UpdatedAt)
	return r, err
}

// ListRecords returns the object's records, newest first.
func (s *CustomFieldService) ListRecords(ctx context.Context, objectID int64) ([]*models.CustomObjectRecord, error) {
	rows, err := s.DB.QueryContext(ctx, s.q("SELECT "+recordColumns+" FROM "+s.t("custom_object_records")+" WHERE custom_object_id = ? ORDER BY id DESC"), objectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*models.CustomObjectRecord{}
	for rows.Next() {
		r, err := scanRecord(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// GetRecord returns the object's record, or nil when the record does not
// exist or belongs to another object.
func (s *CustomFieldService) GetRecord(ctx context.Context, objectID, id int64) (*models.CustomObjectRecord, error) {
	r, err := scanRecord(s.DB.QueryRowContext(ctx, s.q("SELECT "+recordColumns+" FROM "+s.t("custom_object_records")+" WHERE id = ? AND custom_object_id = ?"), id, objectID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return r, err
}

// SaveRecord inserts (ID 0) or updates the record's data.
func (s *CustomFieldService) SaveRecord(ctx context.Context, r *models.CustomObjectRecord, data map[string]any) error {
	b, err := json.Marshal(data)
	if err != nil {
		return err
	}
	r.Data = models.JSONText(b)
	now := time.Now().UTC()
	r.UpdatedAt = now
	if r.ID == 0 {
		r.CreatedAt = now
		return s.DB.QueryRowContext(ctx, s.q("INSERT INTO "+s.t("custom_object_records")+" (custom_object_id, data, created_at, updated_at) VALUES (?, ?, ?, ?) RETURNING id"),
			r.CustomObjectID, string(b), now, now).Scan(&r.ID)
	}
	_, err = s.DB.ExecContext(ctx, s.q("UPDATE "+s.t("custom_object_records")+" SET data = ?, updated_at = ? WHERE id = ? AND custom_object_id = ?"),
		string(b), now, r.ID, r.CustomObjectID)
	return err
}

// DeleteRecord deletes the object's record.
func (s *CustomFieldService) DeleteRecord(ctx context.Context, objectID, id int64) error {
	_, err := s.DB.ExecContext(ctx, s.q("DELETE FROM "+s.t("custom_object_records")+" WHERE id = ? AND custom_object_id = ?"), id, objectID)
	return err
}

// ValidateRecordData checks record data against the object's schema:
// required keys non-empty, number numeric, date a valid date, select one of
// the options, and no unknown keys. Errors are keyed data.<name>.
func ValidateRecordData(schema []ObjectField, data map[string]any) map[string]string {
	errs := map[string]string{}
	known := map[string]ObjectField{}
	for _, f := range schema {
		known[f.Name] = f
	}
	for key := range data {
		if _, ok := known[key]; !ok {
			errs["data."+key] = fmt.Sprintf("The data.%s field is not part of this object.", key)
		}
	}
	for _, f := range schema {
		key := "data." + f.Name
		raw, present := data[f.Name]
		if !present || emptyValue(raw) {
			if f.Required {
				errs[key] = fmt.Sprintf("The %s field is required.", f.Name)
			}
			continue
		}
		str, isString := raw.(string)
		str = strings.TrimSpace(str)
		switch f.Type {
		case "number":
			if _, isNum := raw.(float64); isNum {
				continue
			}
			if _, err := strconv.ParseFloat(str, 64); !isString || err != nil {
				errs[key] = fmt.Sprintf("The %s field must be a number.", f.Name)
			}
		case "date":
			if len(str) > 10 && str[10] == 'T' {
				str = str[:10]
			}
			if _, err := time.Parse("2006-01-02", str); !isString || err != nil {
				errs[key] = fmt.Sprintf("The %s field must be a valid date.", f.Name)
			}
		case "select":
			if !isString || !slices.Contains(f.Options, str) {
				errs[key] = fmt.Sprintf("The selected %s is invalid.", f.Name)
			}
		}
	}
	return errs
}
