package fields

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
)

// GeoPoint represents a geographic coordinate.
type GeoPoint struct {
	Latitude  float64 `json:"lat"`
	Longitude float64 `json:"lon"`
}

// GeoPointField handles geographic coordinates.
// Stored as native PostgreSQL POINT type or as JSONB.
type GeoPointField struct {
	BaseField
	UseJSON bool `json:"use_json,omitempty"` // store as JSONB instead of native POINT
}

func NewGeoPointField(name string) *GeoPointField {
	return &GeoPointField{BaseField: NewBaseField(name, TypeGeoPoint)}
}

func (f *GeoPointField) PGType() string {
	if f.UseJSON {
		return "JSONB"
	}
	return "POINT"
}

func (f *GeoPointField) PGDefault() string { return "" }
func (f *GeoPointField) ColumnDef() string {
	if f.UseJSON {
		return QuoteIdent(f.name) + " " + f.BaseField.commonColumnDef("JSONB", "")
	}
	return QuoteIdent(f.name) + " " + f.BaseField.commonColumnDef("POINT", "")
}

func (f *GeoPointField) Validate(raw any) (any, error) {
	if raw == nil {
		if f.required {
			return nil, fmt.Errorf("field %q is required", f.name)
		}
		return nil, nil
	}

	gp, err := f.parsePoint(raw)
	if err != nil {
		return nil, fmt.Errorf("field %q: %w", f.name, err)
	}

	if gp.Latitude < -90 || gp.Latitude > 90 {
		return nil, fmt.Errorf("field %q: latitude must be between -90 and 90", f.name)
	}
	if gp.Longitude < -180 || gp.Longitude > 180 {
		return nil, fmt.Errorf("field %q: longitude must be between -180 and 180", f.name)
	}

	return gp, nil
}

func (f *GeoPointField) parsePoint(raw any) (*GeoPoint, error) {
	switch v := raw.(type) {
	case GeoPoint:
		return &v, nil
	case *GeoPoint:
		return v, nil
	case map[string]any:
		lat, _ := toFloat(v["lat"])
		lon, _ := toFloat(v["lon"])
		if n, ok := v["latitude"]; ok {
			if l, ok := toFloat(n); ok {
				lat = l
			}
		}
		if n, ok := v["longitude"]; ok {
			if l, ok := toFloat(n); ok {
				lon = l
			}
		}
		if n, ok := v["lng"]; ok {
			if l, ok := toFloat(n); ok {
				lon = l
			}
		}
		return &GeoPoint{Latitude: lat, Longitude: lon}, nil
	case string:
		return parsePointString(v)
	case []byte:
		// Try JSON first
		var gp GeoPoint
		if err := json.Unmarshal(v, &gp); err == nil {
			return &gp, nil
		}
		return parsePointString(string(v))
	default:
		// Try JSON marshal/unmarshal
		b, err := json.Marshal(v)
		if err != nil {
			return nil, fmt.Errorf("cannot parse geo point: %T", raw)
		}
		var gp GeoPoint
		if err := json.Unmarshal(b, &gp); err != nil {
			return nil, fmt.Errorf("cannot parse geo point: %v", raw)
		}
		return &gp, nil
	}
}

func parsePointString(s string) (*GeoPoint, error) {
	s = strings.TrimSpace(s)

	// Try PostgreSQL POINT format: (lon,lat)
	if strings.HasPrefix(s, "(") && strings.HasSuffix(s, ")") {
		s = s[1 : len(s)-1]
	}

	// Try "lat,lon" format
	parts := strings.Split(s, ",")
	if len(parts) >= 2 {
		lat, err1 := parseFloat(strings.TrimSpace(parts[0]))
		lon, err2 := parseFloat(strings.TrimSpace(parts[1]))
		if err1 == nil && err2 == nil {
			// Handle both (lat,lon) and (lon,lat) - assume lat,lon
			if math.Abs(lat) <= 90 && math.Abs(lon) <= 180 {
				return &GeoPoint{Latitude: lat, Longitude: lon}, nil
			}
			// Try lon,lat
			if math.Abs(lon) <= 90 && math.Abs(lat) <= 180 {
				return &GeoPoint{Latitude: lon, Longitude: lat}, nil
			}
		}
	}

	return nil, fmt.Errorf("cannot parse geo point string: %q", s)
}

func parseFloat(s string) (float64, error) {
	var f float64
	_, err := fmt.Sscanf(s, "%f", &f)
	return f, err
}

func (f *GeoPointField) Marshal(value any) (any, error) {
	gp, err := f.Validate(value)
	if err != nil {
		return nil, err
	}
	if gp == nil {
		return nil, nil
	}
	pt := gp.(*GeoPoint)

	if f.UseJSON {
		b, _ := json.Marshal(pt)
		return b, nil
	}
	// PostgreSQL POINT format: (lon,lat)
	return fmt.Sprintf("(%f,%f)", pt.Longitude, pt.Latitude), nil
}

func (f *GeoPointField) Unmarshal(raw any) (any, error) {
	if raw == nil {
		return nil, nil
	}

	var gp *GeoPoint
	switch v := raw.(type) {
	case string:
		// Handle PostgreSQL POINT format
		if strings.HasPrefix(v, "(") && strings.HasSuffix(v, ")") {
			inner := v[1 : len(v)-1]
			parts := strings.Split(inner, ",")
			if len(parts) == 2 {
				lon, _ := parseFloat(strings.TrimSpace(parts[0]))
				lat, _ := parseFloat(strings.TrimSpace(parts[1]))
				gp = &GeoPoint{Latitude: lat, Longitude: lon}
			}
		} else {
			// Try JSON
			json.Unmarshal([]byte(v), &gp)
		}
	case []byte:
		json.Unmarshal(v, &gp)
	default:
		b, _ := json.Marshal(v)
		json.Unmarshal(b, &gp)
	}

	if gp == nil {
		return nil, nil
	}
	return gp, nil
}

func (f *GeoPointField) Clone() Field {
	clone := *f
	clone.options = make(map[string]any)
	for k, v := range f.options {
		clone.options[k] = v
	}
	return &clone
}

func (f *GeoPointField) SetOptions(opts map[string]any) {
	f.BaseField.SetOptions(opts)
	if v, ok := opts["use_json"]; ok {
		f.UseJSON, _ = v.(bool)
	}
}
