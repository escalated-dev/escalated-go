package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"
)

// isInertia reports whether the request is an Inertia visit, as the shared
// pages make their writes (useForm, router.post).
func isInertia(r *http.Request) bool {
	return r.Header.Get("X-Inertia") != ""
}

// seeOther ends an Inertia write the way Laravel's redirect() does: a 303, so
// the browser follows with a GET.
func seeOther(w http.ResponseWriter, r *http.Request, location string) {
	http.Redirect(w, r, location, http.StatusSeeOther)
}

// readInput reads a write's fields from a JSON body or a form (urlencoded or
// multipart), so the same handler serves API clients and the pages' useForm
// posts. JSON numbers and booleans become their string form; arrays become
// repeated values.
func readInput(r *http.Request) (url.Values, error) {
	ct, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	switch ct {
	case "application/x-www-form-urlencoded":
		if err := r.ParseForm(); err != nil {
			return nil, err
		}
		return r.PostForm, nil
	case "multipart/form-data":
		if err := r.ParseMultipartForm(32 << 20); err != nil {
			return nil, err
		}
		return r.MultipartForm.Value, nil
	}

	out := url.Values{}
	if r.Body == nil {
		return out, nil
	}
	var raw map[string]any
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		if errors.Is(err, io.EOF) {
			return out, nil
		}
		return nil, err
	}
	for k, v := range raw {
		switch val := v.(type) {
		case nil:
			out.Set(k, "")
		case []any:
			for _, item := range val {
				out.Add(k, inputString(item))
			}
		default:
			out.Set(k, inputString(val))
		}
	}
	return out, nil
}

func inputString(v any) string {
	switch val := v.(type) {
	case nil:
		return ""
	case string:
		return val
	case bool:
		return strconv.FormatBool(val)
	case float64:
		return strconv.FormatFloat(val, 'f', -1, 64)
	default:
		b, _ := json.Marshal(val)
		return string(b)
	}
}

// validationErrors answers a failed validation: the page again with an errors
// prop for an Inertia visit (what useForm reads), else 422 with Laravel's
// {message, errors} JSON.
func validationErrors(w http.ResponseWriter, r *http.Request, errs map[string]string, rerender func(errs map[string]string)) {
	if isInertia(r) && rerender != nil {
		rerender(errs)
		return
	}
	first := ""
	for _, msg := range errs {
		first = msg
		break
	}
	fields := map[string][]string{}
	for k, msg := range errs {
		fields[k] = []string{msg}
	}
	writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"message": first, "errors": fields})
}

func requiredMessage(field string) string {
	return fmt.Sprintf("The %s field is required.", field)
}
