package core

import "encoding/json"

// ToJSON encodes the alerts of linted files as the CLI's `--output=JSON`
// does: a map from each file's path to its alerts, sorted by position.
func ToJSON(linted []*File) ([]byte, error) {
	out := map[string][]Alert{}
	for _, f := range linted {
		if f == nil {
			continue
		}
		alerts := f.SortedAlerts()
		if alerts == nil {
			// A clean file is an empty list, not null.
			alerts = []Alert{}
		}
		out[f.Path] = alerts
	}
	return json.Marshal(out)
}
