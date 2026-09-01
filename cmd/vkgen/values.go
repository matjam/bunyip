package main

import (
	"fmt"
	"strconv"
	"strings"
)

// enumValue turns one <enum> element into a Go constant. extNumber is the
// enclosing extension's number, used when the element does not carry its own.
func enumValue(en enumEntry, extNumber string) (enumVal, error) {
	v := enumVal{Name: en.Name}
	switch {
	case en.Alias != "":
		v.Alias = en.Alias
	case en.Value != "":
		v.Expr = en.Value
	case en.BitPos != "":
		v.Expr = "1 << " + en.BitPos
	case en.Offset != "":
		num := en.ExtNumber
		if num == "" {
			num = extNumber
		}
		n, err := strconv.Atoi(num)
		if err != nil {
			return v, fmt.Errorf("%s: bad extension number %q", en.Name, num)
		}
		off, err := strconv.Atoi(en.Offset)
		if err != nil {
			return v, fmt.Errorf("%s: bad offset %q", en.Name, en.Offset)
		}
		val := int64(1000000000 + (n-1)*1000 + off)
		if en.Dir == "-" {
			val = -val
		}
		v.Expr = strconv.FormatInt(val, 10)
	default:
		return v, fmt.Errorf("%s: no value, bitpos, offset or alias", en.Name)
	}
	return v, nil
}

// constExpr converts an API-constant value such as "(~0U)", "256" or "1000.0F"
// into a Go expression, returning the integer value where it has one.
func constExpr(value, ctype string) (expr string, n int64, err error) {
	s := strings.TrimSpace(value)
	if strings.HasPrefix(s, `"`) {
		return s, 0, nil
	}
	s = strings.TrimPrefix(s, "(")
	s = strings.TrimSuffix(s, ")")
	if strings.HasPrefix(s, "~") {
		body := strings.TrimRight(s[1:], "ULul")
		i, err := strconv.ParseInt(body, 0, 64)
		if err != nil {
			return "", 0, fmt.Errorf("value %q: %w", value, err)
		}
		var width int64 = 1<<32 - 1
		if ctype == "uint64_t" {
			width = -1
		}
		// Emit an untyped literal so the constant converts to VkDeviceSize and friends.
		return fmt.Sprintf("0x%X", uint64(width^i)), width ^ i, nil
	}
	if ctype == "float" {
		return strings.TrimRight(s, "Ff"), 0, nil
	}
	body := strings.TrimRight(s, "ULul")
	i, err := strconv.ParseInt(body, 0, 64)
	if err != nil {
		return "", 0, fmt.Errorf("value %q: %w", value, err)
	}
	return body, i, nil
}
