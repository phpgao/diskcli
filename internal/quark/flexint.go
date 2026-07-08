package quark

import (
	"encoding/json"
	"strconv"
)

// FlexInt is a numeric type that accepts both int and string.
//
// The code field type differs across Quark endpoints:
//   - /account/info: "code": "OK" (string)
//   - /1/clouddrive/*: "code": 0 (number)
//
// FlexInt handles both cases; "OK" is parsed as 0 (the success marker).
type FlexInt int

// UnmarshalJSON accepts both numbers and strings.
func (f *FlexInt) UnmarshalJSON(b []byte) error {
	// Try parsing as a JSON string.
	if len(b) >= 2 && b[0] == '"' && b[len(b)-1] == '"' {
		s := string(b[1 : len(b)-1])
		// Non-numeric strings such as "OK" are treated as 0 (success).
		if n, err := strconv.Atoi(s); err == nil {
			*f = FlexInt(n)
			return nil
		}
		*f = 0
		return nil
	}
	// Parse as a JSON number.
	var n json.Number
	if err := json.Unmarshal(b, &n); err != nil {
		// Other types (bool/null) are treated as 0.
		*f = 0
		return nil
	}
	i, err := n.Int64()
	if err != nil {
		*f = 0
		return nil
	}
	*f = FlexInt(i)
	return nil
}

// Int converts to int.
func (f FlexInt) Int() int { return int(f) }

// IsZero reports whether the value is 0 (success).
func (f FlexInt) IsZero() bool { return f == 0 }
