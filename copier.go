package copier

import (
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"unicode"
)

// These flags define options for tag handling
const (
	// Denotes that a destination field must be copied to. If copying fails then a panic will ensue.
	tagMust uint8 = 1 << iota

	// Denotes that the program should not panic when the must flag is on and
	// value is not copied. The program will return an error instead.
	tagNoPanic

	// Ignore a destination field from being copied to.
	tagIgnore

	// Denotes that the value as been copied
	hasCopied
)

// Option sets copy options
type Option struct {
	// setting this value to true will ignore copying zero values of all the fields, including bools, as well as a
	// struct having all it's fields set to their zero values respectively (see IsZero() in reflect/value.go)
	IgnoreEmpty   bool
	DeepCopy      bool
	IgnorePrivate bool
	HookFunc      []HookFunc
	ParseFunc     []ParseFunc

	// private
	destNames TagNameMap
	srcNames  TagNameMap
}

type TagNameMap struct {
	FieldNameToTag map[string]string
	TagToFieldName map[string]string
}

// proceed true: proceed, false: do not continue copy processing
type HookFunc func(value reflect.Value, field reflect.StructField) (proceed bool)

// copied true: do not continue copy processing
type ParseFunc func(to, from reflect.Value) (copied bool, err error)

// Copy copy things
func Copy(toValue interface{}, fromValue interface{}) (err error) {
	return copier(toValue, fromValue, Option{})
}

// CopyWithOption copy with option
func CopyWithOption(toValue interface{}, fromValue interface{}, opt Option) (err error) {
	return copier(toValue, fromValue, opt)
}

func copier(toValue interface{}, fromValue interface{}, opt Option) (err error) {
	var (
		from = indirect(reflect.ValueOf(fromValue))
		to   = indirect(reflect.ValueOf(toValue))
	)

	if !to.CanAddr() {
		return ErrInvalidCopyDestination
	}

	// Return is from value is invalid
	if !from.IsValid() {
		return ErrInvalidCopyFrom
	}

	fromType, _ := indirectType(from.Type())
	toType, _ := indirectType(to.Type())

	if fromType.Kind() == reflect.Interface {
		fromType = reflect.TypeOf(from.Interface())
	}

	if toType.Kind() == reflect.Interface {
		toType = reflect.TypeOf(to.Interface())
	}

	// Just set it if possible to assign for normal types
	if from.Kind() != reflect.Slice &&
		from.Kind() != reflect.Struct &&
		from.Kind() != reflect.Map &&
		(from.Type().AssignableTo(to.Type()) || from.Type().ConvertibleTo(to.Type())) {
		return copyNormalType(to, from, opt)
	}

	if fromType.Kind() == reflect.Map && toType.Kind() == reflect.Map {
		return copyMapType(to, from, opt)
	}

	if from.Kind() == reflect.Slice && to.Kind() == reflect.Slice && fromType.ConvertibleTo(toType) {
		return copySliceType(to, from, opt)
	}

	if fromType.Kind() == reflect.Struct && toType.Kind() == reflect.Struct {
		return copyStructType(to, from, opt)
	}

	// skip not supported type
	return
}

func shouldIgnore(v reflect.Value, field reflect.StructField, ignoreEmpty, ignorePrivate bool) bool {
	if ignoreEmpty {
		return v.IsZero()
	}
	if ignorePrivate {
		return unicode.IsLower([]rune(field.Name)[0])
	}
	return false
}

func hookFunc(v reflect.Value, field reflect.StructField, funcs []HookFunc) bool {
	for _, f := range funcs {
		if !f(v, field) {
			return false
		}
	}
	return true
}

func copyNormalType(to, from reflect.Value, opt Option) error {
	// custom parse func
	for _, f := range opt.ParseFunc {
		if proceed, err := f(to, from); err != nil {
			return err
		} else {
			if !proceed {
				return nil
			}
		}
	}
	var isPtrFrom bool
	for from.Type().Kind() == reflect.Ptr || from.Type().Kind() == reflect.Slice {
		isPtrFrom = true
	}
	if !isPtrFrom || !opt.DeepCopy {
		to.Set(from.Convert(to.Type()))
	} else {
		fromCopy := reflect.New(from.Type())
		fromCopy.Set(from.Elem())
		to.Set(fromCopy.Convert(to.Type()))
	}
	return nil
}

func copyMapType(to, from reflect.Value, opt Option) error {
	var (
		fromType = from.Type()
		toType   = to.Type()
	)
	// custom parse func
	for _, f := range opt.ParseFunc {
		if proceed, err := f(to, from); err != nil {
			return err
		} else {
			if !proceed {
				return nil
			}
		}
	}

	if !fromType.Key().ConvertibleTo(toType.Key()) {
		return ErrMapKeyNotMatch
	}

	if to.IsNil() {
		to.Set(reflect.MakeMapWithSize(toType, from.Len()))
	}

	for _, k := range from.MapKeys() {
		toKey := indirect(reflect.New(toType.Key()))
		if !set(toKey, k, opt.DeepCopy, opt.ParseFunc) {
			return fmt.Errorf("%w map, old key: %v, new key: %v", ErrNotSupported, k.Type(), toType.Key())
		}

		elemType, _ := indirectType(toType.Elem())
		toValue := indirect(reflect.New(elemType))
		if !set(toValue, from.MapIndex(k), opt.DeepCopy, opt.ParseFunc) {
			if err := copier(toValue.Addr().Interface(), from.MapIndex(k).Interface(), opt); err != nil {
				return err
			}
		}

		for {
			if elemType == toType.Elem() {
				to.SetMapIndex(toKey, toValue)
				break
			}
			elemType = reflect.PtrTo(elemType)
			toValue = toValue.Addr()
		}
	}
	return nil
}

func copySliceType(to, from reflect.Value, opt Option) error {
	// custom parse func
	for _, f := range opt.ParseFunc {
		if proceed, err := f(to, from); err != nil {
			return err
		} else {
			if !proceed {
				return nil
			}
		}
	}
	if to.IsNil() {
		slice := reflect.MakeSlice(reflect.SliceOf(to.Type().Elem()), from.Len(), from.Cap())
		to.Set(slice)
	}
	for i := 0; i < from.Len(); i++ {
		if !set(to.Index(i), from.Index(i), opt.DeepCopy, opt.ParseFunc) {
			err := CopyWithOption(to.Index(i).Addr().Interface(), from.Index(i).Interface(), opt)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func copyStructType(to, from reflect.Value, opt Option) error {
	var (
		toType   = to.Type()
		fromType = from.Type()
		source   = indirect(from)
		dest     = indirect(to)
	)
	// custom parse func
	for _, f := range opt.ParseFunc {
		if proceed, err := f(dest, source); err != nil {
			return err
		} else {
			if !proceed {
				return nil
			}
		}
	}

	destKind := dest.Kind()
	if destKind == reflect.Interface {
		dest = indirect(reflect.New(toType))
	}

	// Get tag options
	tagBitFlags := map[string]uint8{}
	if dest.IsValid() {
		var err error
		if tagBitFlags, err = getBitFlags(toType); err != nil {
			return err
		}
		if opt.destNames, err = getNameFlag(toType); err != nil {
			return err
		}
	}

	// check source
	if source.IsValid() {
		var err error
		if opt.srcNames, err = getNameFlag(fromType); err != nil {
			return err
		}
		// Copy from source field to dest field or method
		for _, field := range deepFields(fromType) {
			if err := copyStructFromDest(source, dest, field, tagBitFlags, opt); err != nil {
				return err
			}
		}

		// Copy from from method to dest field
		for _, field := range deepFields(toType) {
			copyStructFromSrcMethod(source, dest, field, opt)
		}
	}
	to.Set(dest)
	return checkBitFlags(tagBitFlags)
}

func copyStructFromDest(source, dest reflect.Value, field reflect.StructField, tagBitFlags map[string]uint8, opt Option) error {
	var (
		srcName  = field.Name
		destName = field.Name
	)

	// get tags field name
	if srcTagName, ok := opt.srcNames.FieldNameToTag[srcName]; ok {
		destName = srcTagName
	}
	if destTagName, ok := opt.destNames.TagToFieldName[destName]; ok {
		destName = destTagName
	}

	// Get bit flags for field
	fieldFlags, _ := tagBitFlags[destName]

	// Check if we should ignore copying
	if (fieldFlags & tagIgnore) != 0 {
		return nil
	}

	fromField := source.FieldByName(srcName)
	if !fromField.IsValid() {
		return nil
	}
	if shouldIgnore(fromField, field, opt.IgnoreEmpty, opt.IgnorePrivate) {
		return nil
	}
	if !hookFunc(fromField, field, opt.HookFunc) {
		return nil
	}

	// process for nested anonymous field
	destFieldNotSet := false

	if f, ok := dest.Type().FieldByName(srcName); ok {
		for idx, x := range f.Index {
			if x >= dest.NumField() {
				continue
			}

			destFieldKind := dest.Field(x).Kind()
			if destFieldKind != reflect.Ptr {
				continue
			}

			if !dest.Field(x).IsNil() {
				continue
			}

			if !dest.Field(x).CanSet() {
				destFieldNotSet = true
				break
			}

			newValue := reflect.New(dest.FieldByIndex(f.Index[0 : idx+1]).Type().Elem())
			dest.Field(x).Set(newValue)
		}
	}

	if destFieldNotSet {
		return nil
	}

	toField := dest.FieldByName(destName)
	if toField.IsValid() {
		if toField.CanSet() {
			if !set(toField, fromField, opt.DeepCopy, opt.ParseFunc) {
				if err := copier(toField.Addr().Interface(), fromField.Interface(), opt); err != nil {
					return err
				}
			} else {
				if fieldFlags != 0 {
					// Note that a copy was made
					tagBitFlags[destName] = fieldFlags | hasCopied
				}
			}
		}
	} else {
		// try to set to method
		var toMethod reflect.Value
		if dest.CanAddr() {
			toMethod = dest.Addr().MethodByName(destName)
		} else {
			toMethod = dest.MethodByName(destName)
		}

		if toMethod.IsValid() && toMethod.Type().NumIn() == 1 && fromField.Type().AssignableTo(toMethod.Type().In(0)) {
			toMethod.Call([]reflect.Value{fromField})
		}
	}
	return nil
}

func copyStructFromSrcMethod(source, dest reflect.Value, field reflect.StructField, opt Option) {
	var (
		srcName  = field.Name
		destName = field.Name
	)

	if destTagName, ok := opt.destNames.FieldNameToTag[destName]; ok {
		srcName = destTagName
	}
	if srcFieldName, ok := opt.srcNames.TagToFieldName[srcName]; ok {
		srcName = srcFieldName
	}

	var fromMethod reflect.Value
	if source.CanAddr() {
		fromMethod = source.Addr().MethodByName(srcName)
	} else {
		fromMethod = source.MethodByName(srcName)
	}

	if fromMethod.IsValid() && fromMethod.Type().NumIn() == 0 && fromMethod.Type().NumOut() == 1 &&
		!shouldIgnore(fromMethod, field, opt.IgnoreEmpty, opt.IgnorePrivate) &&
		hookFunc(fromMethod, field, opt.HookFunc) {
		if toField := dest.FieldByName(destName); toField.IsValid() && toField.CanSet() {
			values := fromMethod.Call([]reflect.Value{})
			if len(values) >= 1 {
				set(toField, values[0], opt.DeepCopy, opt.ParseFunc)
			}
		}
	}
}

func deepFields(reflectType reflect.Type) []reflect.StructField {
	if reflectType, _ = indirectType(reflectType); reflectType.Kind() == reflect.Struct {
		fields := make([]reflect.StructField, 0, reflectType.NumField())

		for i := 0; i < reflectType.NumField(); i++ {
			v := reflectType.Field(i)
			if v.Anonymous {
				fields = append(fields, deepFields(v.Type)...)
			} else {
				fields = append(fields, v)
			}
		}

		return fields
	}

	return nil
}

func indirect(reflectValue reflect.Value) reflect.Value {
	for reflectValue.Kind() == reflect.Ptr {
		reflectValue = reflectValue.Elem()
	}
	return reflectValue
}

func indirectType(reflectType reflect.Type) (_ reflect.Type, isPtr bool) {
	for reflectType.Kind() == reflect.Ptr || reflectType.Kind() == reflect.Slice {
		reflectType = reflectType.Elem()
		isPtr = true
	}
	return reflectType, isPtr
}

func set(to, from reflect.Value, deepCopy bool, funcs []ParseFunc) bool {
	if from.IsValid() {
		if to.Kind() == reflect.Ptr {
			// set `to` to nil if from is nil
			if from.Kind() == reflect.Ptr && from.IsNil() {
				to.Set(reflect.Zero(to.Type()))
				return true
			} else if to.IsNil() {
				// `from`         -> `to`
				// sql.NullString -> *string
				if fromValuer, ok := driverValuer(from); ok {
					v, err := fromValuer.Value()
					if err != nil {
						return false
					}
					// if `from` is not valid do nothing with `to`
					if v == nil {
						return true
					}
				}
				// allocate new `to` variable with default value (eg. *string -> new(string))
				to.Set(reflect.New(to.Type().Elem()))
			}
			// depointer `to`
			to = to.Elem()
		}

		// custom parse func
		for _, f := range funcs {
			if proceed, err := f(to, from); err != nil {
				return true
			} else {
				if !proceed {
					return true
				}
			}
		}

		if deepCopy {
			toKind := to.Kind()
			if toKind == reflect.Interface && to.IsNil() {
				to.Set(reflect.New(reflect.TypeOf(from.Interface())).Elem())
				toKind = reflect.TypeOf(to.Interface()).Kind()
			}
			if toKind == reflect.Struct || toKind == reflect.Map || toKind == reflect.Slice {
				return false
			}
		}

		if from.Type().ConvertibleTo(to.Type()) {
			to.Set(from.Convert(to.Type()))
		} else if toScanner, ok := to.Addr().Interface().(sql.Scanner); ok {
			// `from`  -> `to`
			// *string -> sql.NullString
			if from.Kind() == reflect.Ptr {
				// if `from` is nil do nothing with `to`
				if from.IsNil() {
					return true
				}
				// depointer `from`
				from = indirect(from)
			}
			// `from` -> `to`
			// string -> sql.NullString
			// set `to` by invoking method Scan(`from`)
			err := toScanner.Scan(from.Interface())
			if err != nil {
				return false
			}
		} else if fromValuer, ok := driverValuer(from); ok {
			// `from`         -> `to`
			// sql.NullString -> string
			v, err := fromValuer.Value()
			if err != nil {
				return false
			}
			// if `from` is not valid do nothing with `to`
			if v == nil {
				return true
			}
			rv := reflect.ValueOf(v)
			if rv.Type().AssignableTo(to.Type()) {
				to.Set(rv)
			}
		} else if from.Kind() == reflect.Ptr {
			return set(to, from.Elem(), deepCopy, funcs)
		} else {
			return false
		}
	}

	return true
}

// parseTags Parses struct tags and returns uint8 bit flags.
func parseTags(tag string) (flags uint8, name string, err error) {
	for _, t := range strings.Split(tag, ",") {
		switch t {
		case "-":
			flags = tagIgnore
			return
		case "must":
			flags = flags | tagMust
		case "nopanic":
			flags = flags | tagNoPanic
		default:
			if unicode.IsUpper([]rune(t)[0]) {
				name = strings.TrimSpace(t)
			} else {
				err = errors.New("copier field name tag must be start Upper case")
			}
		}
	}
	return
}

// getBitFlags Parses struct tags for bit flags.
func getBitFlags(toType reflect.Type) (map[string]uint8, error) {
	flags := map[string]uint8{}
	toTypeFields := deepFields(toType)

	// Get a list dest of tags
	for _, field := range toTypeFields {
		tags := field.Tag.Get("copier")
		if tags != "" {
			var err error
			if flags[field.Name], _, err = parseTags(tags); err != nil {
				return flags, err
			}
		}
	}
	return flags, nil
}

// getNameFlag Parses struct tags for name flags.
func getNameFlag(toType reflect.Type) (TagNameMap, error) {
	flags := TagNameMap{
		FieldNameToTag: map[string]string{},
		TagToFieldName: map[string]string{},
	}
	toTypeFields := deepFields(toType)

	// Get a list dest of tags
	for _, field := range toTypeFields {
		tags := field.Tag.Get("copier")
		if tags != "" {
			if _, name, err := parseTags(tags); err != nil {
				return flags, err
			} else if name != "" {
				flags.FieldNameToTag[field.Name] = name
				flags.TagToFieldName[name] = field.Name
			}
		}
	}
	return flags, nil
}

// checkBitFlags Checks flags for error or panic conditions.
func checkBitFlags(flagsList map[string]uint8) (err error) {
	// Check flag conditions were met
	for name, flags := range flagsList {
		if flags&hasCopied == 0 {
			switch {
			case flags&tagMust != 0 && flags&tagNoPanic != 0:
				err = fmt.Errorf("field %s has must tag but was not copied", name)
				return
			case flags&(tagMust) != 0:
				panic(fmt.Sprintf("Field %s has must tag but was not copied", name))
			}
		}
	}
	return
}

func driverValuer(v reflect.Value) (i driver.Valuer, ok bool) {

	if !v.CanAddr() {
		i, ok = v.Interface().(driver.Valuer)
		return
	}

	i, ok = v.Addr().Interface().(driver.Valuer)
	return
}
