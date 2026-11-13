package httpx

import "time"

// Date marshals a DATE column as "YYYY-MM-DD" (PRD §10.1).
type Date time.Time

func (d Date) MarshalJSON() ([]byte, error) {
	return []byte(`"` + time.Time(d).Format(time.DateOnly) + `"`), nil
}

// DateOf converts a nullable DATE value for responses.
func DateOf(t *time.Time) *Date {
	if t == nil {
		return nil
	}
	d := Date(*t)
	return &d
}
